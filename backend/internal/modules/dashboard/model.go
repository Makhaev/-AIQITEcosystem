package dashboard

type Overview struct {
	ActiveStudents    int      `json:"active_students"`
	ActiveGroups      int      `json:"active_groups"`
	ActiveTeachers    int      `json:"active_teachers"`
	LessonsToday      int      `json:"lessons_today"`
	AttendancePercent *float64 `json:"attendance_percent"`
	OverdueTasks      int      `json:"overdue_tasks"`
}
