-- Add owner to files
ALTER TABLE files ADD COLUMN IF NOT EXISTS owner_id UUID NOT NULL DEFAULT gen_random_uuid();

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_files_owner_id ON files(owner_id);
CREATE INDEX idx_users_email ON users(email);