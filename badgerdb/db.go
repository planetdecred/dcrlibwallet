// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"github.com/dgraph-io/badger"
	"github.com/dgraph-io/badger/options"
)

var closedDB = true

// convertErr wraps a driver-specific error with an error code.
func convertErr(err error) error {
	if err == nil {
		return nil
	}
	var kind errors.Kind
	switch err {
	case badger.ErrValueLogSize, badger.ErrValueThreshold, badger.ErrTxnTooBig, badger.ErrReadOnlyTxn, badger.ErrDiscardedTxn, badger.ErrEmptyKey, badger.ErrThresholdZero,
		badger.ErrRejected, badger.ErrInvalidRequest, badger.ErrManagedTxn, badger.ErrInvalidDump, badger.ErrZeroBandwidth, badger.ErrInvalidLoadingMode, badger.ErrWindowsNotSupported, badger.ErrReplayNeeded, badger.ErrTruncateNeeded:
		kind = errors.Invalid
	case badger.ErrKeyNotFound:
		kind = errors.NotExist
	case badger.ErrConflict, badger.ErrRetry, badger.ErrNoRewrite:
		kind = errors.IO
	}
	return errors.E(kind, err)
}

// transaction represents a database transaction.  It can either by read-only or
// read-write and implements the walletdb Tx interfaces.  The transaction
// provides a root bucket against which all read and writes occur.
type transaction struct {
	badgerTx *badger.Txn
	db       *badger.DB
	buckets  []*Bucket
	writable bool
}

func (tx *transaction) ReadBucket(key []byte) walletdb.ReadBucket {
	if closedDB {
		return nil
	}
	return tx.ReadWriteBucket(key)
}

func (tx *transaction) ReadWriteBucket(key []byte) walletdb.ReadWriteBucket {
	if closedDB {
		return nil
	}

	item, err := tx.badgerTx.Get(key)
	if err != nil {
		return nil
	}
	if item.UserMeta() != metaBucket {
		return nil
	}
	readWriteBucket := &Bucket{txn: tx.badgerTx, prefix: key, dbTransaction: tx}
	tx.buckets = append(tx.buckets, readWriteBucket)
	return readWriteBucket
}

func (tx *transaction) CreateTopLevelBucket(key []byte) (walletdb.ReadWriteBucket, error) {
	if closedDB {
		return nil, errors.E(errors.Invalid)
	}

	bucket, err := newBucket(tx.badgerTx, key, tx)
	if err != nil {
		return nil, err
	}
	tx.buckets = append(tx.buckets, bucket)
	return bucket, nil
}

func (tx *transaction) DeleteTopLevelBucket(key []byte) error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	item, err := tx.badgerTx.Get(key)
	if err != nil {
		return convertErr(err)
	}
	if item.UserMeta() != metaBucket {
		return errors.E(errors.Invalid)
	}

	tx.badgerTx.Delete(item.Key()[:])

	it := tx.badgerTx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	for it.Seek(key); it.ValidForPrefix(key); it.Next() {
		item = it.Item()
		val, err := item.Value()
		if err != nil {
			continue
		}
		prefixLength := int(val[0])
		if bytes.Equal(item.Key()[:prefixLength], key) {
			tx.badgerTx.Delete(item.Key()[:])
		}
	}
	for i := range tx.buckets {
		if bytes.Equal(tx.buckets[i].prefix, key) {
			tx.buckets = append(tx.buckets[:i], tx.buckets[i+1:]...)
			break
		}
	}
	return nil
}

// Commit commits all changes that have been made through the root bucket and
// all of its sub-buckets to persistent storage.
//
// This function is part of the walletdb.Tx interface implementation.
func (tx *transaction) Commit() error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	err := tx.badgerTx.Commit(nil)
	if err != nil {
		return convertErr(err)
	}
	return nil
}

// Rollback undoes all changes that have been made to the root bucket and all of
// its sub-buckets.
//
// This function is part of the walletdb.Tx interface implementation.
func (tx *transaction) Rollback() error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	writeable := tx.writable
	tx.badgerTx.Discard()
	tx.badgerTx = tx.db.NewTransaction(writeable)
	for _, b := range tx.buckets {
		b.setTx(tx.badgerTx)
	}
	return nil
}

// Enforce bucket implements the walletdb Bucket interfaces.
var _ walletdb.ReadWriteBucket = (*Bucket)(nil)

// NestedReadWriteBucket retrieves a nested bucket with the given key.  Returns
// nil if the bucket does not exist.
//
// This function is part of the walletdb.ReadWriteBucket interface implementation.
func (b *Bucket) NestedReadWriteBucket(key []byte) walletdb.ReadWriteBucket {
	if closedDB {
		return nil
	}

	copiedKey := make([]byte, len(key))
	copy(copiedKey, key)
	k, err := addPrefix(b.prefix, copiedKey)
	if err != nil {
		return nil
	}
	item, err := b.txn.Get(k)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil
		}
		return nil
	}
	if item.UserMeta() != metaBucket {
		return nil
	}
	nestedBucket := &Bucket{txn: b.txn, prefix: k, dbTransaction: b.dbTransaction}
	b.dbTransaction.buckets = append(b.dbTransaction.buckets, nestedBucket)
	return nestedBucket
}

func (b *Bucket) NestedReadBucket(key []byte) walletdb.ReadBucket {
	if closedDB {
		return nil
	}
	return b.NestedReadWriteBucket(key)
}

// CreateBucket creates and returns a new nested bucket with the given key.
// Errors with code Exist if the bucket already exists, and Invalid if the key
// is empty or otherwise invalid for the driver.
//
//This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) CreateBucket(key []byte) (walletdb.ReadWriteBucket, error) {
	if closedDB {
		return nil, errors.E(errors.Invalid)
	}
	bucket, err := b.bucket(key, true)
	if err != nil {
		return nil, err
	}
	return bucket, nil
}

// CreateBucketIfNotExists creates and returns a new nested bucket with the
// given key if it does not already exist.  Errors with code Invalid if the key
// is empty or otherwise invalid for the driver.
//
//This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) CreateBucketIfNotExists(key []byte) (walletdb.ReadWriteBucket, error) {
	if closedDB {
		return nil, errors.E(errors.Invalid)
	}
	bucket, err := b.bucket(key, false)
	if err != nil {
		return nil, err
	}
	return bucket, nil
}

// DeleteNestedBucket removes a nested bucket with the given key.
//
//This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) DeleteNestedBucket(key []byte) error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	return b.dropBucket(key[:])
}

// ForEach invokes the passed function with every key/value pair in the bucket.
// This includes nested buckets, in which case the value is nil, but it does not
// include the key/value pairs within those nested buckets.
//
// NOTE: The values returned by this function are only valid during a
// transaction.  Attempting to access them after a transaction has ended will
// likely result in an access violation.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) ForEach(fn func(k, v []byte) error) error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	return convertErr(b.forEach(fn))
}

// Put saves the specified key/value pair to the bucket.  Keys that do not
// already exist are added and keys that already exist are overwritten.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) Put(key, value []byte) error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	return convertErr(b.put(key, value))
}

// Get returns the value for the given key.  Returns nil if the key does
// not exist in this bucket (or nested buckets).
//
// NOTE: The value returned by this function is only valid during a
// transaction.  Attempting to access it after a transaction has ended
// will likely result in an access violation.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) Get(key []byte) []byte {
	if closedDB {
		return nil
	}

	return b.get(key)
}

// Delete removes the specified key from the bucket.  Deleting a key that does
// not exist does not return an error.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) Delete(key []byte) error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	return convertErr((*Bucket)(b).delete(key))
}

func (b *Bucket) ReadCursor() walletdb.ReadCursor {
	if closedDB {
		return nil
	}

	// If transaction is read-only, create a new transaction and return a new cursor
	// This will be changed when the next version of badger gets released.
	if !b.dbTransaction.writable {
		txn := b.dbTransaction.db.NewTransaction(false)
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		reverseOptions := badger.DefaultIteratorOptions
		//Key-only iteration for faster search. Value gets fetched when item.Value() is called.
		reverseOptions.PrefetchValues = false
		reverseOptions.Reverse = true
		txn = b.dbTransaction.db.NewTransaction(false)
		reverseIterator := txn.NewIterator(reverseOptions)
		return &Cursor{iterator: it, reverseIterator: reverseIterator, txn: txn, prefix: b.prefix, dbTransaction: b.dbTransaction}
	}
	return b.ReadWriteCursor()
}

// ReadWriteCursor returns a new cursor, allowing for iteration over the bucket's
// key/value pairs and nested buckets in forward or backward order.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *Bucket) ReadWriteCursor() walletdb.ReadWriteCursor {
	if closedDB {
		return nil
	}
	return b.badgerCursor()
}

// Delete removes the current key/value pair the cursor is at without
// invalidating the cursor.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) Delete() error {
	if closedDB {
		return errors.E(errors.Invalid)
	}

	if c.iterator.ValidForPrefix(c.prefix) {
		item := c.iterator.Item()
		if item.UserMeta() != metaBucket {
			return c.txn.Delete(item.Key())
		}

		return errors.E(errors.Invalid, "cursor points to a nested bucket")
	}
	return nil
}

// First positions the cursor at the first key/value pair and returns the pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) First() (key, value []byte) {
	if closedDB {
		return nil, nil
	}

	c.iterator.Rewind()
	c.iterator.Seek(c.prefix)
	if bytes.Equal(c.prefix, c.iterator.Item().Key()) {
		c.iterator.Next()
	}

	if !c.iterator.ValidForPrefix(c.prefix) {
		return nil, nil
	}

	item := c.iterator.Item()

	val, err := item.ValueCopy(nil)
	if err != nil {
		return nil, nil
	}

	prefixLength := int(val[0])
	if prefixLength == len(c.prefix) {
		if bytes.Equal(item.Key()[:prefixLength], c.prefix) {
			c.ck = item.KeyCopy(nil)
			if item.UserMeta() == metaBucket {
				return c.ck[prefixLength:], nil
			}
			return c.ck[prefixLength:], val[1:]
		}
	}

	//No item found
	return nil, nil
}

// Last positions the cursor at the last key/value pair and returns the pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) Last() (key, value []byte) {
	if closedDB {
		return nil, nil
	}

	var lastValidItem *badger.Item
	c.iterator.Rewind()
	for c.iterator.Seek(c.prefix); c.iterator.ValidForPrefix(c.prefix); c.iterator.Next() {
		item := c.iterator.Item()
		if bytes.Equal(c.prefix, item.Key()) {
			continue
		}
		val, err := item.ValueCopy(nil)
		if err != nil {
			return nil, nil
		}
		prefixLength := int(val[0])
		if bytes.Equal(c.ck[:prefixLength], c.prefix) {
			lastValidItem = item
		}
	}
	if lastValidItem != nil {
		val, err := lastValidItem.ValueCopy(nil)
		if err != nil {
			return nil, nil
		}
		prefixLength := int(val[0])
		c.ck = lastValidItem.KeyCopy(nil)
		if lastValidItem.UserMeta() == metaBucket {
			return c.ck[prefixLength:], nil
		}
		return c.ck[prefixLength:], val[1:]
	}
	return nil, nil
}

// Next moves the cursor one key/value pair forward and returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) Next() (key, value []byte) {
	if closedDB {
		return nil, nil
	}

	if c.ck == nil {
		c.iterator.Seek(c.prefix)
		if bytes.Equal(c.prefix, c.iterator.Item().Key()) {
			c.iterator.Next()
		}
	} else {
		c.iterator.Next()
	}

	if !c.iterator.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	item := c.iterator.Item()

	val, err := item.ValueCopy(nil)
	if err != nil {
		return nil, nil
	}

	prefixLength := int(val[0])
	if prefixLength == len(c.prefix) {
		if bytes.Equal(item.Key()[:prefixLength], c.prefix) {
			c.ck = item.KeyCopy(nil)
			if item.UserMeta() == metaBucket {
				return c.ck[prefixLength:], nil
			}

			return c.ck[prefixLength:], val[1:]
		}
	}

	return nil, nil
}

// Prev moves the cursor one key/value pair backward and returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) Prev() (key, value []byte) {
	if closedDB {
		return nil, nil
	}

	if c.ck == nil {
		c.reverseIterator.Seek(c.prefix)
		if bytes.Equal(c.prefix, c.reverseIterator.Item().Key()) {
			c.reverseIterator.Next()
		}
	} else {
		// Next() is previous in reverse
		c.reverseIterator.Seek(c.ck)
		c.reverseIterator.Next()
	}
	if c.reverseIterator.Valid() {
		c.iterator.Seek(c.reverseIterator.Item().Key())
	}

	if !c.reverseIterator.ValidForPrefix(c.prefix) {
		return nil, nil
	}

	// Get the item from main iterator since item value is already fetched here.
	item := c.reverseIterator.Item()

	val, err := item.ValueCopy(nil)
	if err != nil {
		return nil, nil
	}

	prefixLength := int(val[0])
	if prefixLength == len(c.prefix) {
		if bytes.Equal(item.Key()[:prefixLength], c.prefix) {
			c.ck = item.KeyCopy(nil)
			if item.UserMeta() == metaBucket {
				return c.ck[prefixLength:], nil
			}
			return c.ck[prefixLength:], val[1:]
		}
	}

	//Item Not valid.
	return nil, nil
}

// Seek positions the cursor at the passed seek key. If the key does not exist,
// the cursor is moved to the next key after seek. Returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) Seek(seek []byte) (key, value []byte) {
	if closedDB {
		return nil, nil
	}

	seekKey, err := addPrefix(c.prefix, seek)
	if err != nil {
		return nil, nil
	}
	c.iterator.Seek(seekKey)

	if !c.iterator.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	item := c.iterator.Item()

	val, err := item.ValueCopy(nil)
	if err != nil {
		return nil, nil
	}

	prefixLength := int(val[0])
	if prefixLength == len(c.prefix) {
		if bytes.Equal(item.Key()[:prefixLength], c.prefix) {
			c.ck = item.KeyCopy(nil)
			if item.UserMeta() == metaBucket {
				return c.ck[prefixLength:], nil
			}
			return c.ck[prefixLength:], val[1:]
		}
	}

	return nil, nil
}

// Close the cursor
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *Cursor) Close() {
	if closedDB {
		return
	}

	c.iterator.Close()
}

// db represents a collection of namespaces which are persisted and implements
// the walletdb.Db interface.  All database access is performed through
// transactions which are obtained through the specific Namespace.
type db badger.DB

// Enforce db implements the walletdb.Db interface.
var _ walletdb.DB = (*db)(nil)

func (db *db) beginTx(writable bool) (*transaction, error) {
	if closedDB {
		return nil, errors.E(errors.Invalid)
	}

	tx := (*badger.DB)(db).NewTransaction(writable)
	tran := &transaction{badgerTx: tx, writable: writable, db: (*badger.DB)(db)}
	return tran, nil
}

func (db *db) BeginReadTx() (walletdb.ReadTx, error) {
	return db.beginTx(false)
}

func (db *db) BeginReadWriteTx() (walletdb.ReadWriteTx, error) {
	return db.beginTx(true)
}

// Copy writes a copy of the database to the provided writer.  This call will
// start a read-only transaction to perform all operations.
//
// This function is part of the walletdb.Db interface implementation.
func (db *db) Copy(w io.Writer) error {
	return errors.E(errors.Invalid, "method not implemented")
}

// Close cleanly shuts down the database and syncs all data.
//
// This function is part of the walletdb.Db interface implementation.
func (db *db) Close() error {
	if closedDB {
		return errors.E(errors.Invalid, "database is already closed")
	}

	closedDB = true // setting this to true to pause all operations that will happen while db is closing

	if ticker != nil {
		ticker.Stop()
	}

	time.Sleep(2 * time.Second) // sleep for 2 seconds to ensure any db operation completes before proceeding

	err := (*badger.DB)(db).Close()
	if err != nil {
		return convertErr(err)
	}

	return nil
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

var ticker *time.Ticker

// openDB opens the database at the provided path.
func openDB(dbPath string, create bool) (walletdb.DB, error) {
	if !create && !fileExists(dbPath) {
		return nil, errors.E(errors.NotExist, "missing database file")
	}
	opts := badger.DefaultOptions
	opts.Dir = dbPath
	opts.ValueDir = dbPath
	opts.ValueLogLoadingMode = options.FileIO
	opts.TableLoadingMode = options.MemoryMap
	opts.ValueLogFileSize = 209715200
	opts.MaxTableSize = 40000000
	opts.LevelOneSize = 209715200
	opts.NumMemtables = 1
	opts.NumCompactors = 1
	opts.NumLevelZeroTables = 1
	opts.NumLevelZeroTablesStall = 2

	badgerDb, err := badger.Open(opts)
	if err == nil {
		go func() {
			ticker = time.NewTicker(20 * time.Second)
			for range ticker.C {
			again:
				err := badgerDb.RunValueLogGC(0.7)
				if err == nil {
					goto again
				}
			}
		}()
	}

	closedDB = false

	return (*db)(badgerDb), convertErr(err)
}
