DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'pr_status') THEN
        CREATE TYPE pr_status AS ENUM ('OPEN', 'MERGED');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS teams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    username VARCHAR(50) NOT NULL,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    is_active BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS pull_requests (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    author_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status pr_status NOT NULL DEFAULT 'OPEN',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    merged_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS reviewers (
    pull_request_id UUID NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (pull_request_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_users_team_id_is_active ON users(team_id, is_active);
CREATE INDEX IF NOT EXISTS idx_pull_requests_status_open ON pull_requests(status) WHERE status = 'OPEN';
CREATE INDEX IF NOT EXISTS idx_reviewers_user_id ON reviewers(user_id);
