package access

import (
	"aiq.local/core/internal/platform/httpx"
	"testing"
)

func actor(roles ...string) httpx.Actor { return httpx.Actor{ID: 10, Roles: roles} }
func TestPermissionsDenyByDefaultAndCombineRoles(t *testing.T) {
	for _, tc := range []struct {
		roles    []string
		resource Resource
		action   Action
		scope    Scope
	}{
		{[]string{"admin"}, Users, Manage, All}, {[]string{"leader"}, Users, Read, None},
		{[]string{"admin"}, Teachers, Manage, All}, {[]string{"leader"}, Teachers, Read, All}, {[]string{"leader"}, Teachers, Manage, None},
		{[]string{"teacher"}, Teachers, Read, Own}, {[]string{"curator"}, Teachers, Read, None},
		{[]string{"leader"}, Dashboard, Read, All}, {[]string{"teacher"}, Dashboard, Read, None},
		{[]string{"leader"}, Tasks, Manage, All}, {[]string{"teacher"}, Tasks, Manage, None}, {[]string{"teacher"}, Tasks, ChangeStatus, Own},
		{[]string{"teacher"}, Attendance, Mark, Own}, {[]string{"curator"}, Attendance, Mark, None},
		{[]string{"teacher", "curator"}, Students, Read, Own | Assigned}, {[]string{"admin", "teacher"}, Teachers, Read, All},
		{[]string{"owner"}, Teachers, Manage, None}, {[]string{"admin"}, Resource("unknown"), Read, None}, {[]string{"admin"}, Teachers, Action("delete"), None},
		{nil, Teachers, Read, None},
	} {
		if got := ScopeFor(actor(tc.roles...), tc.resource, tc.action); got != tc.scope {
			t.Errorf("roles=%v resource=%s action=%s got=%d want=%d", tc.roles, tc.resource, tc.action, got, tc.scope)
		}
	}
	missing := actor("admin")
	missing.ID = 0
	forced := actor("admin")
	forced.MustChangePassword = true
	for _, value := range []httpx.Actor{missing, forced} {
		if ScopeFor(value, Teachers, Read) != None {
			t.Fatal("invalid/temporary account gained permission")
		}
	}
}
func TestObjectScopesRejectForeignIDs(t *testing.T) {
	own, other := int64(10), int64(20)
	if !TeacherAllowed(actor("teacher"), &own) || TeacherAllowed(actor("teacher"), &other) || TeacherAllowed(actor("teacher"), nil) {
		t.Fatal("teacher profile scope escaped its owner")
	}
	if !TeacherAllowed(actor("leader"), &other) || TeacherAllowed(actor("curator"), &other) {
		t.Fatal("teacher directory roles incorrect")
	}
	links := StudentLinks{CuratorID: &own, LessonTeacherUserIDs: []int64{20}}
	if !StudentAllowed(actor("curator"), links, true) || StudentAllowed(actor("teacher"), links, true) || StudentAllowed(actor("teacher"), links, false) {
		t.Fatal("full/minimal student scope incorrect")
	}
	links.CuratorID = &other
	links.LessonTeacherUserIDs = []int64{10}
	if StudentAllowed(actor("curator"), links, true) || !StudentAllowed(actor("teacher"), links, false) || StudentAllowed(actor("teacher", "curator"), links, true) {
		t.Fatal("foreign curator scope leaked")
	}
	links.CuratorID = &own
	if !StudentAllowed(actor("teacher", "curator"), links, true) || !StudentAllowed(actor("leader"), StudentLinks{}, true) {
		t.Fatal("role union lost allowed student access")
	}
	if !LessonAllowed(actor("teacher"), &own, true) || LessonAllowed(actor("teacher"), &other, true) || LessonAllowed(actor("leader"), &own, true) || LessonAllowed(actor("curator"), &own, true) {
		t.Fatal("attendance scope escaped lesson teacher")
	}
	if !LessonAllowed(actor("teacher", "curator"), &own, false) || !LessonAllowed(actor("curator"), &other, false, 10) || LessonAllowed(actor("curator"), &other, false, 20) {
		t.Fatal("combined lesson read scopes incorrect")
	}
}
