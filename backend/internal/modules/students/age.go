package students

import (
	"time"
	_ "time/tzdata"
)

// ageOn returns completed years on the Moscow calendar date of now.
// Birth dates are calendar dates, not instants in the server's time zone.
func ageOn(birthDate *string, now time.Time) *int {
	if birthDate == nil {
		return nil
	}
	birth, err := time.Parse("2006-01-02", *birthDate)
	if err != nil {
		return nil
	}
	zone, _ := time.LoadLocation("Europe/Moscow")
	today := now.In(zone)
	age := today.Year() - birth.Year()
	// time.Date normalizes 29 February to 1 March in a non-leap year.
	birthday := time.Date(today.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, zone)
	if today.Before(birthday) {
		age--
	}
	if age < 0 {
		return nil
	}
	return &age
}

type StudentAge struct {
	Age *int `json:"age"`
}
