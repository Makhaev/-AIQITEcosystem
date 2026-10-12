-- Student 360 reads attendance across all original groups/enrollments.
CREATE INDEX attendance_student_history ON attendance(student_id,lesson_id);
