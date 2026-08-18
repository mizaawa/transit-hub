-- 创建安全设置表，用于存储安全入口路径等配置
CREATE TABLE IF NOT EXISTS security_settings (
    user_id text NOT NULL,
    admin_account_id text NOT NULL DEFAULT '',
    security_entry_path text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, admin_account_id)
);

-- 添加索引以提高查询性能
CREATE INDEX IF NOT EXISTS idx_security_settings_user_id ON security_settings(user_id);
