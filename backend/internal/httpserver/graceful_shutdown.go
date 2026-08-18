package httpserver

import (
	"context"
	"log"
	"sync"
	"time"
)

// shutdownManager 管理服务优雅关闭时需要等待的后台任务
type shutdownManager struct {
	tasks   []func(context.Context) error
	mu      sync.Mutex
	timeout time.Duration
}

func newShutdownManager(timeout time.Duration) *shutdownManager {
	return &shutdownManager{
		tasks:   make([]func(context.Context) error, 0),
		timeout: timeout,
	}
}

// RegisterShutdownTask 注册一个需要在关闭时执行的任务
func (sm *shutdownManager) RegisterShutdownTask(task func(context.Context) error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tasks = append(sm.tasks, task)
}

// Shutdown 执行所有注册的关闭任务
func (sm *shutdownManager) Shutdown(ctx context.Context) error {
	sm.mu.Lock()
	tasks := make([]func(context.Context) error, len(sm.tasks))
	copy(tasks, sm.tasks)
	sm.mu.Unlock()

	if len(tasks) == 0 {
		return nil
	}

	log.Printf("[shutdown] executing %d shutdown tasks", len(tasks))

	var wg sync.WaitGroup
	errChan := make(chan error, len(tasks))

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t func(context.Context) error) {
			defer wg.Done()
			if err := t(ctx); err != nil {
				log.Printf("[shutdown] task %d failed: %v", idx+1, err)
				errChan <- err
			} else {
				log.Printf("[shutdown] task %d completed", idx+1)
			}
		}(i, task)
	}

	// 等待所有任务完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(errChan)
		if len(errChan) > 0 {
			return <-errChan
		}
		log.Println("[shutdown] all tasks completed successfully")
		return nil
	case <-ctx.Done():
		log.Println("[shutdown] timeout waiting for tasks to complete")
		return ctx.Err()
	}
}
