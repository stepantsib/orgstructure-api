package domain

import (
	"encoding/json"
	"time"
)

// Department represents an organizational unit in the tree.
type Department struct {
	ID        uint64    `gorm:"primaryKey"            json:"id"`
	Name      string    `gorm:"size:200;not null"     json:"name"`
	ParentID  *uint64   `gorm:"index"                 json:"parent_id"`
	CreatedAt time.Time `gorm:"not null"              json:"created_at"`

	Parent    *Department  `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE" json:"-"`
	Children  []Department `gorm:"foreignKey:ParentID"                              json:"-"`
	Employees []Employee   `gorm:"foreignKey:DepartmentID"                          json:"-"`
}

// TableName aligns GORM with the migration-created table.
func (Department) TableName() string { return "departments" }

// DepartmentTreeNode is the recursive response shape for GET /departments/{id}.
//
// `Employees` is a *pointer* to a slice so we can distinguish three states:
//   - include_employees=false           → nil → field omitted entirely
//   - include_employees=true, has rows  → &[...] → serialized as JSON array
//   - include_employees=true, no rows   → &[]    → serialized as []
//
// A non-pointer slice with `omitempty` cannot tell "empty" from "absent",
// which would hide `employees: []` for empty departments — counter to the spec.
type DepartmentTreeNode struct {
	Department Department            `json:"department"`
	Employees  *[]Employee           `json:"employees,omitempty"`
	Children   []*DepartmentTreeNode `json:"children"`
}

// CreateDepartmentInput is the payload for POST /departments/.
type CreateDepartmentInput struct {
	Name     string  `json:"name"`
	ParentID *uint64 `json:"parent_id,omitempty"`
}

// Nullable distinguishes three states in JSON PATCH bodies:
//   - absent       (Set=false)
//   - present null (Set=true,  Valid=false)
//   - present T    (Set=true,  Valid=true, Value=T)
type Nullable[T any] struct {
	Set   bool
	Valid bool
	Value T
}

// UnmarshalJSON is called only when the field is present in the payload,
// so Set reliably captures presence.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	if string(data) == "null" {
		n.Valid = false
		var zero T
		n.Value = zero
		return nil
	}
	n.Valid = true
	return json.Unmarshal(data, &n.Value)
}

// UpdateDepartmentInput is the payload for PATCH /departments/{id}.
type UpdateDepartmentInput struct {
	Name     Nullable[string] `json:"name,omitempty"`
	ParentID Nullable[uint64] `json:"parent_id,omitempty"`
}

// DeleteMode is the strategy used when deleting a department.
type DeleteMode string

const (
	DeleteModeCascade  DeleteMode = "cascade"
	DeleteModeReassign DeleteMode = "reassign"
)

func (m DeleteMode) Valid() bool {
	return m == DeleteModeCascade || m == DeleteModeReassign
}
