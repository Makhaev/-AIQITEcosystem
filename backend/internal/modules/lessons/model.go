package lessons

import (
	lessonmodel "aiq.local/core/internal/domain/lesson"
	"aiq.local/core/internal/modules/attendance"
)

type Lesson = lessonmodel.Lesson
type Input = lessonmodel.Input
type Optional[T any] = lessonmodel.Optional[T]
type Update = lessonmodel.Update
type Filter = lessonmodel.Filter
type Page = lessonmodel.Page

type Participant = attendance.Participant
type Journal = attendance.Journal
type RosterInput = attendance.RosterInput
type Mark = attendance.Mark
type MarkInput = attendance.MarkInput
