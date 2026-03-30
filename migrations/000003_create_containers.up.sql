CREATE TABLE containers (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id),
    name VARCHAR(255) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    command JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'Created',
    mount_path VARCHAR(255) NOT NULL DEFAULT '/workspace',
    docker_container_id VARCHAR(64) NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, name)
);

CREATE INDEX idx_containers_user_id ON containers (user_id);
CREATE INDEX idx_containers_status ON containers (status);
