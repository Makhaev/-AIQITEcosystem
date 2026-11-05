package app_test

import "testing"

// Family-only fixtures use a real group assignment; no student-level curator is
// accepted by the API. Tests for existing programs/groups assign those directly.
func curatedTestGroup(t *testing.T, admin *browser, studentID, curatorID int64) int64 {
	t.Helper()
	p := programCreate(t, admin, map[string]any{"name": "Сопровождение семьи", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	setGroupCurator(t, admin, g.ID, curatorID)
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": studentID, "starts_on": "2000-01-01"}, 201)
	return g.ID
}
func setGroupCurator(t *testing.T, admin *browser, groupID, curatorID int64) {
	t.Helper()
	var id any
	if curatorID != 0 {
		id = curatorID
	}
	expect(t, admin, "PATCH", groupPath(groupID), map[string]any{"curator_id": id}, 200)
}
