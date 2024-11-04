package sqlite

import (
	"bytes"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/bvinc/go-sqlite-lite/sqlite3"

	corestore "cosmossdk.io/core/store"
)

var _ corestore.Iterator = (*iterator)(nil)

type iterator struct {
	statement  *sqlite3.Stmt
	key, val   []byte
	start, end []byte
	valid      bool
	err        error
}

func newIterator(db *Database, storeKey []byte, version uint64, start, end []byte, reverse bool) (*iterator, error) {
	if isHighBitSet(version) {
		return nil, fmt.Errorf("%d too large; uint64 with the highest bit set are not supported", version)
	}
	if version < db.earliestVersion {
		return &iterator{
			start: start,
			end:   end,
			valid: false,
		}, nil
	}

	var (
		targetVersion = int64(version)
		keyClause     = []string{"store_key = ?", "version <= ?"}
		queryArgs     []any
	)

	switch {
	case len(start) > 0 && len(end) > 0:
		keyClause = append(keyClause, "key >= ?", "key < ?")
		queryArgs = []any{storeKey, targetVersion, start, end, targetVersion}

	case len(start) > 0 && len(end) == 0:
		keyClause = append(keyClause, "key >= ?")
		queryArgs = []any{storeKey, targetVersion, start, targetVersion}

	case len(start) == 0 && len(end) > 0:
		keyClause = append(keyClause, "key < ?")
		queryArgs = []any{storeKey, targetVersion, end, targetVersion}

	default:
		queryArgs = []any{storeKey, targetVersion, targetVersion}
	}

	orderBy := "ASC"
	if reverse {
		orderBy = "DESC"
	}

	// Note, this is not susceptible to SQL injection because placeholders are used
	// for parts of the query outside the store's direct control.
	stmt, err := db.storage.Prepare(fmt.Sprintf(`
	SELECT x.key, x.value
	FROM (
		SELECT key, value, version, tombstone,
			row_number() OVER (PARTITION BY key ORDER BY version DESC) AS _rn
			FROM state_storage WHERE %s
		) x
	WHERE x._rn = 1 AND (x.tombstone = 0 OR x.tombstone > ?) ORDER BY x.key %s;
	`, strings.Join(keyClause, " AND "), orderBy))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare SQL statement: %w", err)
	}

	err = stmt.Bind(queryArgs...)
	if err != nil {
		_ = stmt.Close()
		return nil, fmt.Errorf("failed to bind SQL iterator query arguments: %w", err)
	}

	itr := &iterator{
		statement: stmt,
		start:     start,
		end:       end,
	}
	itr.valid, err = itr.statement.Step()
	if err != nil {
		itr.err = fmt.Errorf("failed to step SQL iterator: %w", err)
		return itr, nil
	}
	if !itr.valid {
		itr.err = fmt.Errorf("iterator invalid: %w", sql.ErrNoRows)
		return itr, nil
	}

	// read the first row
	itr.parseRow()
	if !itr.valid {
		return itr, nil
	}

	return itr, nil
}

func (itr *iterator) Close() (err error) {
	if itr.statement != nil {
		err = itr.statement.Close()
	}

	itr.valid = false
	itr.statement = nil

	return err
}

// Domain returns the domain of the iterator. The caller must not modify the
// return values.
func (itr *iterator) Domain() ([]byte, []byte) {
	return itr.start, itr.end
}

func (itr *iterator) Key() []byte {
	itr.assertIsValid()
	return slices.Clone(itr.key)
}

func (itr *iterator) Value() []byte {
	itr.assertIsValid()
	return slices.Clone(itr.val)
}

func (itr *iterator) Valid() bool {
	if !itr.valid {
		return itr.valid
	}

	// if key is at the end or past it, consider it invalid
	if end := itr.end; end != nil {
		if bytes.Compare(end, itr.Key()) <= 0 {
			itr.valid = false
			return itr.valid
		}
	}

	return true
}

func (itr *iterator) Next() {
	var hasRow bool
	hasRow, itr.err = itr.statement.Step()
	if itr.err != nil || !hasRow {
		itr.valid = false
		return
	}
	itr.parseRow()
}

func (itr *iterator) Error() error {
	return itr.err
}

func (itr *iterator) parseRow() {
	var (
		key   []byte
		value []byte
	)
	if err := itr.statement.Scan(&key, &value); err != nil {
		itr.err = fmt.Errorf("failed to scan row: %w", err)
		itr.valid = false
		return
	}

	itr.key = key
	itr.val = value
}

func (itr *iterator) assertIsValid() {
	if !itr.valid {
		panic("iterator is invalid")
	}
}
