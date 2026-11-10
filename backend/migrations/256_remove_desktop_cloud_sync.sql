-- Desktop cloud sync is permanently removed. Delete only its private cloud data;
-- users, device identities, billing and desktop-local history are unaffected.
-- Keep migration 254 unchanged so deployed migration checksums remain valid.
DROP TABLE IF EXISTS desktop_sync_blobs;
DROP TABLE IF EXISTS desktop_sync_operations;
DROP TABLE IF EXISTS desktop_sync_documents;
DROP TABLE IF EXISTS desktop_sync_accounts;
