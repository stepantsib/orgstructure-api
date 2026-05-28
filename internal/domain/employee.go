package domain

import "time"

// Employee belongs to exactly one department.
type Employee struct {
	ID           uint64    `gorm:"primaryKey"        json:"id"`
	DepartmentID uint64    `gorm:"not null;index"    json:"department_id"`
	FullName     string    `gorm:"size:200;not null" json:"full_name"`
	Position     string    `gorm:"size:200;not null" json:"position"`
	HiredAt      *Date     `gorm:"type:date"         json:"hired_at"`
	CreatedAt    time.Time `gorm:"not null"          json:"created_at"`
}

func (Employee) TableName() string { return "employees" }

// CreateEmployeeInput is the payload for POST /departments/{id}/employees/.
// HiredAt accepts "YYYY-MM-DD" via the custom date type.
type CreateEmployeeInput struct {
	FullName string `json:"full_name"`
	Position string `json:"position"`
	HiredAt  *Date  `json:"hired_at,omitempty"`
}

// EmployeeSortField controls the ordering of employees in GET responses.
type EmployeeSortField string

const (
	SortEmployeesByCreatedAt EmployeeSortField = "created_at"
	SortEmployeesByFullName  EmployeeSortField = "full_name"
)

func (s EmployeeSortField) Valid() bool {
	return s == SortEmployeesByCreatedAt || s == SortEmployeesByFullName
}
