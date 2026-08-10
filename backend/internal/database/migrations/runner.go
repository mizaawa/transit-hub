package migrations

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var migrationFiles embed.FS

// migrationLockID 是 schema 迁移使用的全局 advisory lock 标识。
// 取一个固定的任意常量即可，只要不与其它模块的 advisory lock 冲突。
const migrationLockID int64 = 82331205478

// unlockTimeout 是释放 advisory lock 的超时时间。解锁必须使用独立 context，
// 因此需要一个自己的上限，避免进程退出时无限期阻塞。
const unlockTimeout = 10 * time.Second

// Run 按文件名顺序执行所有未应用的 SQL 迁移。
// 每个迁移在独立事务中执行，已执行的版本记录在 schema_migrations 表中。
//
// 整个过程持有一个 session 级 advisory lock：多副本部署（或滚动升级时新旧容器
// 短暂共存）会同时启动并同时执行迁移。没有锁时两个实例可能同时读到「未应用」，
// 于是并发执行同一个 DDL，导致其中一个因对象已存在而启动失败，甚至留下只完成
// 一半的结构变更。加锁后，后启动的实例会等待；拿到锁后重新读取 schema_migrations，
// 发现已应用便直接跳过。
//
// 注意：加锁、读取已应用版本和执行迁移必须在同一条连接上完成。
// 若中途改用连接池（db.Exec/db.Begin），语句可能落到另一条没有持锁的连接上，
// 锁就形同虚设。
func Run(ctx context.Context, db *pgxpool.Pool) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrations: acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("migrations: acquire advisory lock: %w", err)
	}
	defer func() {
		// 用独立的 context 解锁：调用方的 ctx 可能已被取消，
		// 那样解锁语句会直接失败，锁会一直留到连接关闭为止。
		unlockCtx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
			log.Printf("[migrations] release advisory lock: %v", err)
		}
	}()

	// 先确保 schema_migrations 表存在（不走迁移记录，直接执行）
	if err := ensureMigrationsTable(ctx, conn); err != nil {
		return fmt.Errorf("migrations: create schema_migrations table: %w", err)
	}

	// 读取所有嵌入的 SQL 文件
	entries, err := migrationFiles.ReadDir(".")
	if err != nil {
		return fmt.Errorf("migrations: read embedded dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// 获取已执行的版本集合（必须在持锁的同一条连接上读，才能反映最新状态）
	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return fmt.Errorf("migrations: query applied versions: %w", err)
	}

	for _, file := range files {
		version := strings.TrimSuffix(file, ".sql")
		if applied[version] {
			continue
		}

		sql, err := migrationFiles.ReadFile(file)
		if err != nil {
			return fmt.Errorf("migrations: read file %s: %w", file, err)
		}

		if err := applyMigration(ctx, conn, file, version, string(sql)); err != nil {
			return err
		}

		log.Printf("[migrations] applied %s", file)
	}

	return nil
}

// applyMigration 在单个事务里执行迁移 SQL 并记录版本号，
// 保证「结构已变更但版本未记录」这种半完成状态不会被提交。
func applyMigration(ctx context.Context, conn *pgxpool.Conn, file string, version string, sql string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrations: begin tx for %s: %w", file, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("migrations: exec %s: %w", file, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("migrations: record version %s: %w", file, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrations: commit %s: %w", file, err)
	}
	committed = true
	return nil
}

// ensureMigrationsTable 确保 schema_migrations 表存在。
// 这个建表语句不记录到迁移历史中，因为它是迁移系统自身的基础设施。
func ensureMigrationsTable(ctx context.Context, conn *pgxpool.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	return err
}

// appliedVersions 返回已执行迁移版本的集合
func appliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result[v] = true
	}
	return result, rows.Err()
}
