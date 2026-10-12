package access

import "aiq.local/core/internal/platform/httpx"

type Resource string
type Action string
type Scope uint8

const (
	Users        Resource = "users"
	Teachers     Resource = "teachers"
	Students     Resource = "students"
	Parents      Resource = "parents"
	Groups       Resource = "groups"
	Programs     Resource = "programs"
	Lessons      Resource = "lessons"
	Attendance   Resource = "attendance"
	Tasks        Resource = "tasks"
	Dashboard    Resource = "dashboard"
	Audit        Resource = "audit"
	Read         Action   = "read"
	ReadFull     Action   = "read_full"
	Manage       Action   = "manage"
	Mark         Action   = "mark"
	ChangeStatus Action   = "change_status"
)
const (
	None     Scope = 0
	Own      Scope = 1
	Assigned Scope = 2
	All      Scope = 4
)

// ScopeFor is an explicit allow-list. Unknown roles, actions and resources deny access.
// Own/Assigned require a second check against relationships loaded from the database.
func ScopeFor(actor httpx.Actor, resource Resource, action Action) Scope {
	if actor.ID <= 0 || actor.MustChangePassword {
		return None
	}
	result := None
	for _, role := range actor.Roles {
		scope := roleScope(role, resource, action)
		if scope == All {
			return All
		}
		result |= scope
	}
	return result
}

func roleScope(role string, resource Resource, action Action) Scope {
	switch resource {
	case Users, Audit:
		if role == "admin" && (action == Read || action == Manage && resource == Users) {
			return All
		}
	case Teachers:
		if role == "admin" && (action == Read || action == Manage) || role == "leader" && action == Read {
			return All
		}
		if role == "teacher" && action == Read {
			return Own
		}
	case Students:
		if (role == "admin" || role == "leader") && (action == Read || action == ReadFull) || role == "admin" && action == Manage {
			return All
		}
		if role == "curator" && (action == Read || action == ReadFull) {
			return Assigned
		}
		if role == "teacher" && action == Read {
			return Own
		}
	case Parents:
		if (role == "admin" || role == "leader") && action == Read || role == "admin" && action == Manage {
			return All
		}
		if role == "curator" && action == Read {
			return Assigned
		}
	case Groups, Programs, Lessons:
		if (role == "admin" || role == "leader") && action == Read || role == "admin" && action == Manage {
			return All
		}
		if role == "teacher" && action == Read {
			return Own
		}
		if role == "curator" && action == Read {
			return Assigned
		}
	case Attendance:
		if (role == "admin" || role == "leader") && action == Read || role == "admin" && action == Mark {
			return All
		}
		if role == "teacher" && (action == Read || action == Mark) {
			return Own
		}
		if role == "curator" && action == Read {
			return Assigned
		}
	case Tasks:
		if (role == "admin" || role == "leader") && (action == Read || action == Manage || action == ChangeStatus) {
			return All
		}
		if (role == "teacher" || role == "curator") && (action == Read || action == ChangeStatus) {
			return Own
		}
	case Dashboard:
		if (role == "admin" || role == "leader") && action == Read {
			return All
		}
	}
	return None
}

func TeacherAllowed(actor httpx.Actor, userID *int64) bool {
	scope := ScopeFor(actor, Teachers, Read)
	return scope == All || scope == Own && userID != nil && *userID == actor.ID
}

type StudentLinks struct {
	CuratorID            *int64
	LessonTeacherUserIDs []int64
}

func StudentAllowed(actor httpx.Actor, links StudentLinks, full bool) bool {
	action := Read
	if full {
		action = ReadFull
	}
	if ScopeFor(actor, Students, action) == All {
		return true
	}
	// Check each role independently: combining teacher and curator must preserve both scopes.
	for _, role := range actor.Roles {
		single := actor
		single.Roles = []string{role}
		switch ScopeFor(single, Students, action) {
		case Assigned:
			if links.CuratorID != nil && *links.CuratorID == actor.ID {
				return true
			}
		case Own:
			for _, id := range links.LessonTeacherUserIDs {
				if id == actor.ID {
					return true
				}
			}
		}
	}
	return false
}

func LessonAllowed(actor httpx.Actor, teacherUserID *int64, markAttendance bool, studentCuratorIDs ...int64) bool {
	resource, action := Lessons, Read
	if markAttendance {
		resource, action = Attendance, Mark
	}
	scope := ScopeFor(actor, resource, action)
	if scope == All || scope&Own != 0 && teacherUserID != nil && *teacherUserID == actor.ID {
		return true
	}
	if scope&Assigned != 0 && !markAttendance {
		for _, id := range studentCuratorIDs {
			if id == actor.ID {
				return true
			}
		}
	}
	return false
}
