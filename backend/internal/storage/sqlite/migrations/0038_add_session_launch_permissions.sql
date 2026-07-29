-- +goose Up
ALTER TABLE sessions ADD COLUMN launch_permissions TEXT NOT NULL DEFAULT ''
    CHECK (launch_permissions IN ('', 'default', 'accept-edits', 'auto', 'bypass-permissions'));

-- +goose Down
ALTER TABLE sessions DROP COLUMN launch_permissions;
