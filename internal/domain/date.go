package domain

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// Date is a YYYY-MM-DD wrapper around time.Time used both in JSON payloads
// and for the DATE column on employees.hired_at. It implements:
//   - json.Marshaler / Unmarshaler (date-only string, "null" when zero)
//   - driver.Valuer / sql.Scanner   (round-trip with Postgres DATE)
type Date struct {
	time.Time
}

const dateLayout = "2006-01-02"

// JSON ----------------------------------------------------------------------

func (d Date) MarshalJSON() ([]byte, error) {
	if d.Time.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.Time.Format(dateLayout) + `"`), nil
}

func (d *Date) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		d.Time = time.Time{}
		return nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("invalid date %q (want YYYY-MM-DD): %w", s, err)
	}
	d.Time = t
	return nil
}

// SQL -----------------------------------------------------------------------

// Value implements driver.Valuer: persists as either NULL or a time.Time
// (which the pgx driver renders into a DATE column without timezone noise).
func (d Date) Value() (driver.Value, error) {
	if d.Time.IsZero() {
		return nil, nil
	}
	return d.Time, nil
}

// Scan implements sql.Scanner. The pgx Postgres driver returns DATE values
// as time.Time, but we also accept string/[]byte for portability.
func (d *Date) Scan(value any) error {
	if value == nil {
		d.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		d.Time = v
	case []byte:
		t, err := time.Parse(dateLayout, string(v))
		if err != nil {
			return fmt.Errorf("scan Date from bytes %q: %w", v, err)
		}
		d.Time = t
	case string:
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			return fmt.Errorf("scan Date from string %q: %w", v, err)
		}
		d.Time = t
	default:
		return fmt.Errorf("cannot scan %T into Date", value)
	}
	return nil
}
