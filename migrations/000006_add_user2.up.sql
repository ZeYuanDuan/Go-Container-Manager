-- Add user2 for isolation testing (user1 uploads, user2 tries to access)
-- Same password "password123", same bcrypt hash as user1/admin
INSERT INTO users (id, username, password_hash) VALUES
    ('7ba7b810-9dad-11d1-80b4-00c04fd430c9', 'user2', '$2a$10$BszzbhH0YA/41My7QEZ1VeJP2s8a2UM4vT2aMW9tH3ZowpkiAnQRG')
ON CONFLICT (username) DO NOTHING;
