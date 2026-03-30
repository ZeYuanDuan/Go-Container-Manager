-- Seed users: user1 and admin, password "password123"
-- bcrypt hash generated with bcrypt.DefaultCost
INSERT INTO users (id, username, password_hash) VALUES
    ('550e8400-e29b-41d4-a716-446655440000', 'admin', '$2a$10$BszzbhH0YA/41My7QEZ1VeJP2s8a2UM4vT2aMW9tH3ZowpkiAnQRG'),
    ('6ba7b810-9dad-11d1-80b4-00c04fd430c8', 'user1', '$2a$10$BszzbhH0YA/41My7QEZ1VeJP2s8a2UM4vT2aMW9tH3ZowpkiAnQRG')
ON CONFLICT (username) DO NOTHING;
