package students

import (
	"aiq.local/core/internal/domain/people"
)

type Optional[T any] = people.Optional[T]
type Student = people.Student
type StudentIdentity = people.StudentIdentity
type StudentPrivateFields = people.StudentPrivateFields
type StudentResponse = people.StudentResponse
type Parent = people.Parent
type StudentInput = people.StudentInput
type StudentUpdate = people.StudentUpdate
type ParentInput = people.ParentInput
type ParentUpdate = people.ParentUpdate
type LinkInput = people.LinkInput
type LinkUpdate = people.LinkUpdate
type Link = people.Link
type ParentLink = people.ParentLink
type StudentLink = people.StudentLink
type StudentCard = people.StudentCard
type StudentFamilyFields = people.StudentFamilyFields
type StudentCardResponse = people.StudentCardResponse
type ParentCard = people.ParentCard
type Filter = people.Filter
type Page[T any] = people.Page[T]

func studentResponse(v Student, full bool) StudentResponse {
	result := StudentResponse{StudentIdentity: StudentIdentity{ID: v.ID, Name: v.Name, Status: v.Status}}
	if full {
		result.StudentPrivateFields = &StudentPrivateFields{BirthDate: v.BirthDate, CuratorID: v.CuratorID, CuratorName: v.CuratorName, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	}
	return result
}
