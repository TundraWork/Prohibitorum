package contract

import "time"

// AppManagerView is one application-manager assignment shown on an admin
// application's manager surface.
type AppManagerView struct {
	ID          int32     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Disabled    bool      `json:"disabled"`
	AssignedAt  time.Time `json:"assignedAt"`
}
