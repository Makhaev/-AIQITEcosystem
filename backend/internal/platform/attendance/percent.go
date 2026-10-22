package attendance

// Percent uses all recorded marks, including excused absences. Unmarked and
// excluded roster rows must be filtered out by the caller.
func Percent(present, late, absent, excused int) *float64 {
	total := present + late + absent + excused
	if total == 0 {
		return nil
	}
	value := float64(present+late) / float64(total) * 100
	return &value
}
