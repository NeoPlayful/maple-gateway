-- 平台用户表：admins 改名 users（资源语义从"管理员"泛化为"平台用户"）。
ALTER TABLE admins RENAME TO users;
