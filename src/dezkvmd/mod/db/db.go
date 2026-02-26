package db

import (
	"sync"

	"github.com/boltdb/bolt"
)

// DB wraps a BoltDB instance and provides bucket-based key-value operations.
// It includes a RWMutex for safe concurrent access.
type DB struct {
	db *bolt.DB
	mu sync.RWMutex
}

// NewDB opens a new BoltDB database at the given path.
func NewDB(path string) (*DB, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	return &DB{db: db}, nil
}

// NewBucket creates a new bucket if it doesn't exist.
func (d *DB) NewBucket(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(id))
		return err
	})
}

// RemoveBucket deletes a bucket.
func (d *DB) RemoveBucket(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket([]byte(id))
	})
}

// BucketExists checks if a bucket exists.
func (d *DB) BucketExists(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var exists bool
	d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(id))
		exists = b != nil
		return nil
	})
	return exists
}

// KeyExists checks if a key exists in a bucket.
func (d *DB) KeyExists(bucketID, key string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var exists bool
	d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketID))
		if b != nil {
			v := b.Get([]byte(key))
			exists = v != nil
		}
		return nil
	})
	return exists
}

// Write sets a key-value pair in a bucket, creating the bucket if it doesn't exist.
func (d *DB) Write(bucketID, key string, value []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketID))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), value)
	})
}

// Read gets the value for a key in a bucket.
func (d *DB) Read(bucketID, key string) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var value []byte
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketID))
		if b != nil {
			value = b.Get([]byte(key))
		}
		return nil
	})
	return value, err
}

// Delete removes a key from a bucket.
func (d *DB) Delete(bucketID, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketID))
		if b != nil {
			return b.Delete([]byte(key))
		}
		return nil
	})
}

// List iterates over all key-value pairs in a bucket using the provided walker function.
func (d *DB) List(bucketID string, walker func(key, value []byte) error) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketID))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			return walker(k, v)
		})
	})
}

// Close closes the underlying BoltDB database.
func (d *DB) Close() error {
	return d.db.Close()
}
