package model

import "time"

// Date is a calendar day stored as days since 2000-01-01. It is four bytes
// instead of the 24 a time.Time costs, which matters when the world holds tens
// of thousands of fixtures, contract dates and injury records.
type Date int32

var epoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func NewDate(year, month, day int) Date {
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return Date(t.Sub(epoch).Hours() / 24)
}

func (d Date) Time() time.Time { return epoch.AddDate(0, 0, int(d)) }

func (d Date) Year() int  { return d.Time().Year() }
func (d Date) Month() int { return int(d.Time().Month()) }
func (d Date) Day() int   { return d.Time().Day() }

func (d Date) Weekday() time.Weekday { return d.Time().Weekday() }

// AddDays returns the date n days later.
func (d Date) AddDays(n int) Date { return d + Date(n) }

// Format renders the date as "Sat 22 Aug 2026".
func (d Date) Format() string { return d.Time().Format("Mon 2 Jan 2006") }

// Short renders the date as "22 Aug".
func (d Date) Short() string { return d.Time().Format("2 Jan") }

// SeasonYear returns the year the season containing this date started. Football
// seasons run July to June, so anything before July belongs to the prior year.
func (d Date) SeasonYear() int {
	t := d.Time()
	if t.Month() < time.July {
		return t.Year() - 1
	}
	return t.Year()
}

// SeasonLabel renders the season as "2026/27".
func SeasonLabel(startYear int) string {
	return time.Date(startYear, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") + "/" +
		time.Date(startYear+1, 1, 1, 0, 0, 0, 0, time.UTC).Format("06")
}
