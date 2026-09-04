// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CardType marks whether a card is a plain card or one that produces further
// occurrences of itself. An absent value means CardTypeNormal.
type CardType string

const (
	CardTypeNormal    CardType = "normal"
	CardTypeRecurring CardType = "recurring"
)

// RecurrenceMode selects what makes the next occurrence appear.
type RecurrenceMode string

const (
	// RecurrenceModeSchedule produces occurrences on a wall-clock schedule.
	RecurrenceModeSchedule RecurrenceMode = "schedule"
	// RecurrenceModeAfterDone produces an occurrence a fixed number of days after
	// the card reaches the column that counts as completion.
	RecurrenceModeAfterDone RecurrenceMode = "afterDone"
)

// RecurrenceHistoryMode selects whether completing a card leaves a record behind.
type RecurrenceHistoryMode string

const (
	// RecurrenceHistoryNewInstance leaves the source card where it is and creates a
	// fresh copy in the target column, accumulating a history of occurrences.
	RecurrenceHistoryNewInstance RecurrenceHistoryMode = "newInstance"
	// RecurrenceHistoryReturnSame moves the source card back to the target column,
	// keeping the board small but recording no history.
	RecurrenceHistoryReturnSame RecurrenceHistoryMode = "returnSame"
)

// RecurrenceRuleKind is the period a schedule rule repeats on.
type RecurrenceRuleKind string

const (
	RecurrenceRuleDaily   RecurrenceRuleKind = "daily"
	RecurrenceRuleWeekly  RecurrenceRuleKind = "weekly"
	RecurrenceRuleMonthly RecurrenceRuleKind = "monthly"
)

// Keys under which the two fields live inside a card block's Fields map.
const (
	CardFieldCardType   = "cardType"
	CardFieldRecurrence = "recurrence"

	// CardFieldRecurrenceSourceID is set on an occurrence and holds the id of the
	// card whose rule produced it. The occurrence carries no rule of its own, so
	// that it cannot recur, but the reference lets the done-column trigger find the
	// rule that governs it. Without it an "afterDone" recurrence would stop after a
	// single occurrence, because the source card never leaves the done column.
	CardFieldRecurrenceSourceID = "recurrenceSourceId"
)

// Field identifiers reported on an ErrInvalidRecurrence. They are the JSON paths
// of the fields, so that a settings form can map a problem back to its input.
const (
	RecurrenceFieldMode          = "mode"
	RecurrenceFieldGroupProperty = "groupPropertyId"
	RecurrenceFieldTargetOption  = "targetOptionId"
	RecurrenceFieldTimezone      = "timezone"
	RecurrenceFieldTime          = "time"
	RecurrenceFieldHistoryMode   = "historyMode"
	RecurrenceFieldStartAt       = "startAt"
	RecurrenceFieldRule          = "rule"
	RecurrenceFieldRuleKind      = "rule.kind"
	RecurrenceFieldInterval      = "rule.interval"
	RecurrenceFieldWeekdays      = "rule.weekdays"
	RecurrenceFieldDayOfMonth    = "rule.dayOfMonth"
	RecurrenceFieldDoneOption    = "doneOptionId"
	RecurrenceFieldDelayDays     = "delayDays"
)

const (
	// RecurrenceWeekdayMonday and RecurrenceWeekdaySunday bound the ISO-8601
	// weekday numbering used on the wire. Note that this is not time.Weekday,
	// which numbers Sunday as zero.
	RecurrenceWeekdayMonday = 1
	RecurrenceWeekdaySunday = 7

	// RecurrenceDayOfMonthLastDay is reserved for a future "last day of the month"
	// encoding. Validation rejects it for now, so that the meaning of dayOfMonth
	// cannot change under configurations written today.
	RecurrenceDayOfMonthLastDay = -1

	minRecurrenceInterval   = 1
	minRecurrenceDelayDays  = 1
	minRecurrenceDayOfMonth = 1
	maxRecurrenceDayOfMonth = 31

	recurrenceDaysPerWeek = 7

	// recurrenceClockLayout is the only accepted spelling of a time of day.
	recurrenceClockLayout = "15:04"

	// maxWallClockGapMinutes bounds the forward scan used when a wall clock does
	// not exist. The longest gap any zone has ever had is a whole day, when
	// Pacific/Kiritimati and Pacific/Apia crossed the date line.
	maxWallClockGapMinutes = 2 * 24 * 60

	// maxRecurrenceCandidateScan bounds the search for the next occurrence. A valid rule
	// finds one within a handful of steps; the bound exists only so that a
	// configuration that slipped past validation cannot loop forever.
	maxRecurrenceCandidateScan = 400
	maxRecurrenceDayScan       = 8
)

var (
	// ErrNilRecurrence is returned when a nil configuration reaches a function
	// that requires one.
	ErrNilRecurrence = errors.New("recurrence configuration is nil")

	// ErrNilCardFields is returned when there is no Fields map to write into.
	ErrNilCardFields = errors.New("card fields map is nil")

	// ErrNoNextOccurrence is returned when the bounded search for the next
	// occurrence found nothing. A valid configuration cannot produce this.
	ErrNoNextOccurrence = errors.New("no next occurrence found within the search bound")

	// ErrNilRecurringCard is returned when a nil row reaches the store.
	ErrNilRecurringCard = errors.New("recurring card is nil")
)

// RecurrenceRule describes the period of a schedule-mode recurrence.
// swagger:model
type RecurrenceRule struct {
	// The period this rule repeats on
	// required: true
	Kind RecurrenceRuleKind `json:"kind"`

	// Repeat every Interval periods. One or greater.
	// required: true
	Interval int `json:"interval"`

	// The days of the week an occurrence falls on, for a weekly rule.
	// ISO-8601 numbering: 1 is Monday and 7 is Sunday.
	// required: false
	Weekdays []int `json:"weekdays,omitempty"`

	// The day of the month an occurrence falls on, for a monthly rule. A month
	// that is too short clamps to its last day.
	// required: false
	DayOfMonth int `json:"dayOfMonth,omitempty"`
}

// RecurrenceConfig is the recurrence configuration of a card. It lives under the
// "recurrence" key of a card block's schemaless Fields map, alongside "cardType".
//
// webapp/src/blocks/card.ts carries a structurally identical TypeScript type.
// The two must be changed together.
// swagger:model
type RecurrenceConfig struct {
	// Whether the recurrence is running. False is a legal paused state, and a
	// paused configuration is allowed to be incomplete.
	// required: true
	Enabled bool `json:"enabled"`

	// What makes the next occurrence appear
	// required: true
	Mode RecurrenceMode `json:"mode"`

	// The id of the select property whose options are the columns
	// required: true
	GroupPropertyID string `json:"groupPropertyId"`

	// The id of the option new occurrences appear in
	// required: true
	TargetOptionID string `json:"targetOptionId"`

	// The IANA timezone name every wall-clock time in this configuration is read in
	// required: true
	Timezone string `json:"timezone"`

	// The time of day an occurrence appears, as HH:MM in Timezone. Applies to
	// both modes.
	// required: true
	Time string `json:"time"`

	// Whether completing a card creates a copy or moves the card itself
	// required: true
	HistoryMode RecurrenceHistoryMode `json:"historyMode"`

	// The phase anchor, in milliseconds since the epoch. Set to the moment the
	// recurrence was enabled and preserved across edits, so that changing the
	// interval or the weekdays does not shift the cycle.
	// required: true
	StartAt int64 `json:"startAt"`

	// The schedule rule. Required when Mode is "schedule".
	// required: false
	Rule *RecurrenceRule `json:"rule,omitempty"`

	// The id of the option that counts as completion. Required when Mode is
	// "afterDone".
	// required: false
	DoneOptionID string `json:"doneOptionId,omitempty"`

	// How many days after completion the next occurrence appears. One or greater.
	// Required when Mode is "afterDone".
	// required: false
	DelayDays int `json:"delayDays,omitempty"`
}

// ErrInvalidRecurrence reports one problem with a recurrence configuration. Field
// is the JSON path of the offending field and Reason is a sentence fit to show to
// whoever is filling the form in.
type ErrInvalidRecurrence struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (e ErrInvalidRecurrence) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// ErrRecurrenceInvalid aggregates every problem found in one pass, so that a form
// can show all of them at once instead of one per round trip.
type ErrRecurrenceInvalid struct {
	Problems []ErrInvalidRecurrence
}

func (e ErrRecurrenceInvalid) Error() string {
	reasons := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		reasons = append(reasons, p.Error())
	}
	return "invalid recurrence configuration: " + strings.Join(reasons, "; ")
}

// ErrUnresolvableWallClock is returned when no instant at all could be found for a
// wall-clock reading in a timezone. Only a corrupt timezone database can cause it.
type ErrUnresolvableWallClock struct {
	Timezone string
	Clock    string
}

func (e ErrUnresolvableWallClock) Error() string {
	return fmt.Sprintf("cannot resolve wall clock %s in timezone %s to an instant", e.Clock, e.Timezone)
}

// IsRecurrenceActive is the single predicate for "this card produces further
// occurrences". The scheduler, the done-column trigger and the REST layer must all
// key off this function rather than spelling the conjunction out again.
func IsRecurrenceActive(cardType CardType, cfg *RecurrenceConfig) bool {
	return cardType == CardTypeRecurring && cfg != nil && cfg.Enabled
}

func recurrenceProblem(field, reason string) ErrInvalidRecurrence {
	return ErrInvalidRecurrence{Field: field, Reason: reason}
}

// Problems returns every problem with the configuration, in a stable order.
//
// It always runs every check, whatever Enabled is set to. A half-configured but
// disabled card is legal to store, and the settings form needs the whole list of
// what is still missing rather than the first thing that is wrong. Deciding
// whether a write may proceed is CheckCardRecurrenceWritable's job.
func (cfg *RecurrenceConfig) Problems() []ErrInvalidRecurrence {
	if cfg == nil {
		return nil
	}

	problems := cfg.commonProblems()

	switch cfg.Mode {
	case RecurrenceModeSchedule:
		problems = append(problems, cfg.scheduleProblems()...)
	case RecurrenceModeAfterDone:
		problems = append(problems, cfg.afterDoneProblems()...)
	default:
		// commonProblems has already reported the unknown mode, and nothing
		// mode-specific can be checked without knowing which mode was meant.
	}

	return problems
}

func (cfg *RecurrenceConfig) commonProblems() []ErrInvalidRecurrence {
	problems := make([]ErrInvalidRecurrence, 0, 6)

	if cfg.Mode != RecurrenceModeSchedule && cfg.Mode != RecurrenceModeAfterDone {
		problems = append(problems, recurrenceProblem(RecurrenceFieldMode,
			fmt.Sprintf("must be %q or %q", RecurrenceModeSchedule, RecurrenceModeAfterDone)))
	}

	if cfg.GroupPropertyID == "" {
		problems = append(problems, recurrenceProblem(RecurrenceFieldGroupProperty,
			"a group-by property is required; it is the property whose options are the columns"))
	}

	if cfg.TargetOptionID == "" {
		problems = append(problems, recurrenceProblem(RecurrenceFieldTargetOption,
			"a target column is required; it is where new occurrences appear"))
	}

	problems = append(problems, cfg.timezoneProblems()...)

	if _, _, err := parseRecurrenceClock(cfg.Time); err != nil {
		problems = append(problems, recurrenceProblem(RecurrenceFieldTime,
			`must be a 24-hour time of day written as HH:MM, for example "09:00"`))
	}

	if cfg.HistoryMode != RecurrenceHistoryNewInstance && cfg.HistoryMode != RecurrenceHistoryReturnSame {
		problems = append(problems, recurrenceProblem(RecurrenceFieldHistoryMode,
			fmt.Sprintf("must be %q or %q", RecurrenceHistoryNewInstance, RecurrenceHistoryReturnSame)))
	}

	return problems
}

func (cfg *RecurrenceConfig) timezoneProblems() []ErrInvalidRecurrence {
	if cfg.Timezone == "" {
		return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldTimezone,
			`a timezone is required, as an IANA name such as "Europe/Moscow"`)}
	}

	// "Local" and "UTC" load successfully but "Local" means whatever the server
	// host happens to be set to, which is not a property of the card.
	if cfg.Timezone == "Local" {
		return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldTimezone,
			`must be an IANA name such as "Europe/Moscow"; "Local" depends on the server and is not allowed`)}
	}

	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldTimezone,
			fmt.Sprintf("%q is not a known IANA timezone name", cfg.Timezone))}
	}

	return nil
}

func (cfg *RecurrenceConfig) scheduleProblems() []ErrInvalidRecurrence {
	problems := make([]ErrInvalidRecurrence, 0, 4)

	if cfg.StartAt <= 0 {
		problems = append(problems, recurrenceProblem(RecurrenceFieldStartAt,
			"a phase anchor is required; set it to the moment the recurrence was enabled"))
	}

	if cfg.DoneOptionID != "" {
		problems = append(problems, recurrenceProblem(RecurrenceFieldDoneOption,
			`only applies when mode is "afterDone"`))
	}

	if cfg.DelayDays != 0 {
		problems = append(problems, recurrenceProblem(RecurrenceFieldDelayDays,
			`only applies when mode is "afterDone"`))
	}

	if cfg.Rule == nil {
		problems = append(problems, recurrenceProblem(RecurrenceFieldRule,
			`a rule is required when mode is "schedule"`))
		return problems
	}

	return append(problems, cfg.Rule.problems()...)
}

func (cfg *RecurrenceConfig) afterDoneProblems() []ErrInvalidRecurrence {
	problems := make([]ErrInvalidRecurrence, 0, 3)

	if cfg.DoneOptionID == "" {
		problems = append(problems, recurrenceProblem(RecurrenceFieldDoneOption,
			`a done column is required when mode is "afterDone"; it is the column that counts as completion`))
	}

	if cfg.DelayDays < minRecurrenceDelayDays {
		problems = append(problems, recurrenceProblem(RecurrenceFieldDelayDays,
			"must be at least 1 day, so that a completed card is visible before the next occurrence appears"))
	}

	if cfg.Rule != nil {
		problems = append(problems, recurrenceProblem(RecurrenceFieldRule,
			`only applies when mode is "schedule"`))
	}

	return problems
}

func (r *RecurrenceRule) problems() []ErrInvalidRecurrence {
	problems := make([]ErrInvalidRecurrence, 0, 4)

	if r.Interval < minRecurrenceInterval {
		problems = append(problems, recurrenceProblem(RecurrenceFieldInterval, "must be at least 1"))
	}

	switch r.Kind {
	case RecurrenceRuleDaily:
		problems = append(problems, r.unusedWeekdaysProblems()...)
		problems = append(problems, r.unusedDayOfMonthProblems()...)
	case RecurrenceRuleWeekly:
		problems = append(problems, r.weekdayProblems()...)
		problems = append(problems, r.unusedDayOfMonthProblems()...)
	case RecurrenceRuleMonthly:
		problems = append(problems, r.dayOfMonthProblems()...)
		problems = append(problems, r.unusedWeekdaysProblems()...)
	default:
		problems = append(problems, recurrenceProblem(RecurrenceFieldRuleKind,
			`must be "daily", "weekly" or "monthly"`))
	}

	return problems
}

func (r *RecurrenceRule) weekdayProblems() []ErrInvalidRecurrence {
	if len(r.Weekdays) == 0 {
		return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldWeekdays,
			"a weekly rule needs at least one day of the week")}
	}

	problems := make([]ErrInvalidRecurrence, 0, 2)
	seen := make(map[int]bool, len(r.Weekdays))

	for _, weekday := range r.Weekdays {
		if weekday < RecurrenceWeekdayMonday || weekday > RecurrenceWeekdaySunday {
			problems = append(problems, recurrenceProblem(RecurrenceFieldWeekdays,
				fmt.Sprintf("%d is not a day of the week; days run from 1 for Monday to 7 for Sunday", weekday)))
			continue
		}
		if seen[weekday] {
			problems = append(problems, recurrenceProblem(RecurrenceFieldWeekdays,
				fmt.Sprintf("day %d is listed more than once", weekday)))
			continue
		}
		seen[weekday] = true
	}

	return problems
}

func (r *RecurrenceRule) dayOfMonthProblems() []ErrInvalidRecurrence {
	if r.DayOfMonth == RecurrenceDayOfMonthLastDay {
		return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldDayOfMonth,
			"the last day of the month is not supported yet; choose a day from 1 to 31")}
	}

	if r.DayOfMonth < minRecurrenceDayOfMonth || r.DayOfMonth > maxRecurrenceDayOfMonth {
		return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldDayOfMonth,
			"a monthly rule needs a day of the month from 1 to 31; a month too short for it uses its last day")}
	}

	return nil
}

func (r *RecurrenceRule) unusedWeekdaysProblems() []ErrInvalidRecurrence {
	if len(r.Weekdays) == 0 {
		return nil
	}
	return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldWeekdays,
		fmt.Sprintf("only applies to a weekly rule, but this rule is %q", r.Kind))}
}

func (r *RecurrenceRule) unusedDayOfMonthProblems() []ErrInvalidRecurrence {
	if r.DayOfMonth == 0 {
		return nil
	}
	return []ErrInvalidRecurrence{recurrenceProblem(RecurrenceFieldDayOfMonth,
		fmt.Sprintf("only applies to a monthly rule, but this rule is %q", r.Kind))}
}

// CheckValid returns an error listing every problem with the configuration, or nil
// when it is complete and self-consistent. It does not look at Enabled.
func (cfg *RecurrenceConfig) CheckValid() error {
	problems := cfg.Problems()
	if len(problems) == 0 {
		return nil
	}
	return ErrRecurrenceInvalid{Problems: problems}
}

// CardRecurrenceProblems returns every problem with the two card fields as they
// appear on a card, including the ones only visible when the pair is considered
// together. As with Problems, it reports and never gates.
func CardRecurrenceProblems(cardType CardType, cfg *RecurrenceConfig) []ErrInvalidRecurrence {
	problems := make([]ErrInvalidRecurrence, 0, 8)

	switch cardType {
	case CardTypeNormal, "":
		if cfg != nil && cfg.Enabled {
			problems = append(problems, recurrenceProblem(CardFieldCardType,
				`a card of type "normal" cannot have recurrence enabled`))
		}
		// A leftover configuration on a normal card is deliberately tolerated, so
		// that switching the type back and forth in the settings form does not
		// discard what was already filled in.
	case CardTypeRecurring:
		if cfg == nil {
			return append(problems, recurrenceProblem(CardFieldRecurrence,
				"a recurring card must carry a recurrence configuration"))
		}
	default:
		problems = append(problems, recurrenceProblem(CardFieldCardType,
			`must be "normal" or "recurring"`))
	}

	return append(problems, cfg.Problems()...)
}

// CheckCardRecurrenceWritable is the write gate for the two card fields.
//
// Validation always runs in full, but it is only enforced when the configuration
// is enabled. A disabled card may therefore be saved half configured, so the
// settings form can keep a work in progress, while a card can never be enabled in
// a broken state. Every path that writes these fields, and in particular every
// path that flips enabled to true, must call this.
func CheckCardRecurrenceWritable(cardType CardType, cfg *RecurrenceConfig) error {
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	problems := CardRecurrenceProblems(cardType, cfg)
	if len(problems) == 0 {
		return nil
	}

	return ErrRecurrenceInvalid{Problems: problems}
}

// NextRunAt returns the next occurrence of cfg strictly after from, as a UTC
// epoch-milliseconds value.
//
// The contract, which the catch-up loop that advances next_run_at depends on:
//
//   - the result is always strictly greater than from, so that feeding a result
//     back in always makes progress;
//   - the result always falls on a whole minute;
//   - an occurrence fires at the earliest instant at or after its nominal
//     wall-clock time in cfg.Timezone, and fires exactly once. Around a daylight
//     saving transition that means a reading the clock skipped fires at the
//     instant the clock reached it, and a reading the clock repeated fires on the
//     first pass.
//
// In "afterDone" mode, from is the instant the card was completed and the result
// is cfg.Time on the day cfg.DelayDays days later. In "schedule" mode the phase of
// an interval greater than one is measured from cfg.StartAt, and no occurrence is
// ever produced before it.
//
// The configuration must be valid whatever Enabled is set to; NextRunAt validates
// it in full and returns the aggregated error if it is not.
func NextRunAt(cfg *RecurrenceConfig, from time.Time) (int64, error) {
	if cfg == nil {
		return 0, ErrNilRecurrence
	}

	if err := cfg.CheckValid(); err != nil {
		return 0, err
	}

	loc, locErr := time.LoadLocation(cfg.Timezone)
	if locErr != nil {
		return 0, fmt.Errorf("cannot load timezone %q: %w", cfg.Timezone, locErr)
	}

	hour, minute, clockErr := parseRecurrenceClock(cfg.Time)
	if clockErr != nil {
		return 0, clockErr
	}

	var next time.Time
	var err error

	switch cfg.Mode {
	case RecurrenceModeAfterDone:
		next, err = nextAfterDone(cfg, from, loc, hour, minute)
	case RecurrenceModeSchedule:
		next, err = nextScheduled(cfg, from, loc, hour, minute)
	default:
		// Unreachable: CheckValid has already rejected any other mode.
		return 0, ErrRecurrenceInvalid{Problems: []ErrInvalidRecurrence{
			recurrenceProblem(RecurrenceFieldMode, "unknown mode"),
		}}
	}

	if err != nil {
		return 0, err
	}

	return GetMillisForTime(next), nil
}

func nextAfterDone(cfg *RecurrenceConfig, from time.Time, loc *time.Location, hour, minute int) (time.Time, error) {
	completed := civilDateOf(from.In(loc))

	// DelayDays is at least one, so the day always advances and the result is
	// always after from. The extra steps only ever run for a historic zone change
	// that moved a wall clock backwards by a whole day.
	for extra := 0; extra <= maxRecurrenceDayScan; extra++ {
		day := completed.addDays(cfg.DelayDays + extra)
		candidate, err := instantAtWallClock(loc, day.year, day.month, day.day, hour, minute)
		if err != nil {
			return time.Time{}, err
		}
		if candidate.After(from) {
			return candidate, nil
		}
	}

	return time.Time{}, ErrNoNextOccurrence
}

func nextScheduled(cfg *RecurrenceConfig, from time.Time, loc *time.Location, hour, minute int) (time.Time, error) {
	start := GetTimeForMillis(cfg.StartAt).In(loc)

	switch cfg.Rule.Kind {
	case RecurrenceRuleDaily:
		return nextDaily(cfg.Rule, from, start, loc, hour, minute)
	case RecurrenceRuleWeekly:
		return nextWeekly(cfg.Rule, from, start, loc, hour, minute)
	case RecurrenceRuleMonthly:
		return nextMonthly(cfg.Rule, from, start, loc, hour, minute)
	default:
		// Unreachable: CheckValid has already rejected any other kind.
		return time.Time{}, ErrNoNextOccurrence
	}
}

// acceptable reports whether a candidate occurrence may be returned: it must be
// strictly after from, and never before the moment the recurrence was enabled.
func acceptableOccurrence(candidate, from, start time.Time) bool {
	return candidate.After(from) && !candidate.Before(start)
}

func nextDaily(rule *RecurrenceRule, from, start time.Time, loc *time.Location, hour, minute int) (time.Time, error) {
	anchor := civilDateOf(start)

	// Snap back to the most recent day that is on the interval, so that an
	// occurrence later on today is still considered.
	steps := recurrenceDaysBetween(anchor, civilDateOf(from.In(loc)))
	if steps < 0 {
		steps = 0
	}
	steps -= steps % rule.Interval

	for i := 0; i <= maxRecurrenceCandidateScan; i++ {
		day := anchor.addDays(steps + i*rule.Interval)
		candidate, err := instantAtWallClock(loc, day.year, day.month, day.day, hour, minute)
		if err != nil {
			return time.Time{}, err
		}
		if acceptableOccurrence(candidate, from, start) {
			return candidate, nil
		}
	}

	return time.Time{}, ErrNoNextOccurrence
}

func nextWeekly(rule *RecurrenceRule, from, start time.Time, loc *time.Location, hour, minute int) (time.Time, error) {
	weekdays := sortedWeekdays(rule.Weekdays)
	anchorWeek := civilDateOf(start).mondayOf()

	weeks := recurrenceDaysBetween(anchorWeek, civilDateOf(from.In(loc)).mondayOf()) / recurrenceDaysPerWeek
	if weeks < 0 {
		weeks = 0
	}
	weeks -= weeks % rule.Interval

	for i := 0; i <= maxRecurrenceCandidateScan; i++ {
		weekStart := anchorWeek.addDays((weeks + i*rule.Interval) * recurrenceDaysPerWeek)
		for _, weekday := range weekdays {
			day := weekStart.addDays(weekday - RecurrenceWeekdayMonday)
			candidate, err := instantAtWallClock(loc, day.year, day.month, day.day, hour, minute)
			if err != nil {
				return time.Time{}, err
			}
			if acceptableOccurrence(candidate, from, start) {
				return candidate, nil
			}
		}
	}

	return time.Time{}, ErrNoNextOccurrence
}

func nextMonthly(rule *RecurrenceRule, from, start time.Time, loc *time.Location, hour, minute int) (time.Time, error) {
	anchor := civilDateOf(start)

	months := recurrenceMonthsBetween(anchor, civilDateOf(from.In(loc)))
	if months < 0 {
		months = 0
	}
	months -= months % rule.Interval

	for i := 0; i <= maxRecurrenceCandidateScan; i++ {
		year, month := shiftMonth(anchor.year, anchor.month, months+i*rule.Interval)

		// A month too short for the requested day uses its last day. DayOfMonth
		// itself is never rewritten, so a rule on the 31st still falls on the 31st
		// in the months that have one.
		day := rule.DayOfMonth
		if last := lastDayOfMonth(year, month); day > last {
			day = last
		}

		candidate, err := instantAtWallClock(loc, year, month, day, hour, minute)
		if err != nil {
			return time.Time{}, err
		}
		if acceptableOccurrence(candidate, from, start) {
			return candidate, nil
		}
	}

	return time.Time{}, ErrNoNextOccurrence
}

// atWallClock returns the instant a wall-clock reading corresponds to in loc,
// resolving the two cases in which a reading cannot be taken literally.
//
// A daylight saving transition can make a reading ambiguous, in which case two
// instants display it and the earlier is returned, or non-existent, in which case
// none does and the instant the clock reached the reading is returned. Both follow
// from the single rule that an occurrence fires at the earliest instant at or
// after its nominal wall-clock time, and fires exactly once.
//
// time.Date cannot be used for either. Its behaviour is explicitly undocumented in
// both cases and is in fact inconsistent between zones: for a non-existent 02:30 it
// returns 03:30 in Europe/Berlin but 01:30 in America/New_York, which is before the
// time that was asked for; for an ambiguous reading it returns the second pass in
// Europe/Berlin and the first in America/New_York.
func instantAtWallClock(loc *time.Location, year int, month time.Month, day, hour, minute int) (time.Time, error) {
	for offset := 0; offset <= maxWallClockGapMinutes; offset++ {
		nominal := time.Date(year, month, day, hour, minute+offset, 0, 0, time.UTC)
		if instant, ok := earliestInstantForWallClock(loc, nominal); ok {
			return instant, nil
		}
	}

	return time.Time{}, ErrUnresolvableWallClock{
		Timezone: loc.String(),
		Clock:    time.Date(year, month, day, hour, minute, 0, 0, time.UTC).Format(time.RFC3339),
	}
}

// wallClockOffsetProbes are the offsets from a nominal instant at which the zone offset
// is sampled. Probing a long way out on both sides catches a transition however
// close to the reading it falls.
var wallClockOffsetProbes = []time.Duration{-48 * time.Hour, 0, 48 * time.Hour}

// earliestInstantFor returns the earliest instant whose wall clock in loc reads the
// same as nominal reads in UTC, and whether any instant does at all.
func earliestInstantForWallClock(loc *time.Location, nominal time.Time) (time.Time, bool) {
	var earliest time.Time
	found := false

	// Every instant displaying this reading is the nominal instant minus one of the
	// offsets loc uses around it, so building a candidate per sampled offset and
	// keeping the ones that really display it enumerates them all.
	for _, probe := range wallClockOffsetProbes {
		_, offset := nominal.Add(probe).In(loc).Zone()
		candidate := time.Unix(nominal.Unix()-int64(offset), 0).In(loc)

		if !sameWallClockReading(candidate, nominal) {
			continue
		}
		if !found || candidate.Before(earliest) {
			earliest = candidate
			found = true
		}
	}

	return earliest, found
}

func sameWallClockReading(a, b time.Time) bool {
	aYear, aMonth, aDay := a.Date()
	bYear, bMonth, bDay := b.Date()

	return aYear == bYear && aMonth == bMonth && aDay == bDay &&
		a.Hour() == b.Hour() && a.Minute() == b.Minute()
}

// parseClock parses an HH:MM 24-hour time of day. It is deliberately strict:
// time.Parse also accepts spellings such as "9:00", and letting those through would
// put two spellings of the same time of day into stored configurations.
func parseRecurrenceClock(clock string) (int, int, error) {
	parsed, err := time.Parse(recurrenceClockLayout, clock)
	if err != nil || parsed.Format(recurrenceClockLayout) != clock {
		return 0, 0, recurrenceProblem(RecurrenceFieldTime,
			fmt.Sprintf("%q is not a time of day written as HH:MM", clock))
	}

	return parsed.Hour(), parsed.Minute(), nil
}

func sortedWeekdays(weekdays []int) []int {
	sorted := make([]int, len(weekdays))
	copy(sorted, weekdays)
	sort.Ints(sorted)

	return sorted
}

// civil is a calendar date with no timezone attached. All date arithmetic runs on
// these, so that a daylight saving transition can never add or drop a day.
type civilDate struct {
	year  int
	month time.Month
	day   int
}

func civilDateOf(t time.Time) civilDate {
	year, month, day := t.Date()
	return civilDate{year: year, month: month, day: day}
}

func (c civilDate) utc() time.Time {
	return time.Date(c.year, c.month, c.day, 0, 0, 0, 0, time.UTC)
}

func (c civilDate) addDays(days int) civilDate {
	return civilDateOf(time.Date(c.year, c.month, c.day+days, 0, 0, 0, 0, time.UTC))
}

// isoWeekday returns 1 for Monday through 7 for Sunday, the numbering used on the
// wire. time.Weekday numbers Sunday as zero and cannot be used directly.
func (c civilDate) isoWeekday() int {
	weekday := int(c.utc().Weekday())
	if weekday == int(time.Sunday) {
		return RecurrenceWeekdaySunday
	}

	return weekday
}

func (c civilDate) mondayOf() civilDate {
	return c.addDays(-(c.isoWeekday() - RecurrenceWeekdayMonday))
}

// daysBetween returns the number of whole days from a to b, negative when b is the
// earlier of the two.
func recurrenceDaysBetween(a, b civilDate) int {
	return int(b.utc().Sub(a.utc()) / (24 * time.Hour))
}

func recurrenceMonthsBetween(a, b civilDate) int {
	return (b.year-a.year)*12 + int(b.month) - int(a.month)
}

func shiftMonth(year int, month time.Month, months int) (int, time.Month) {
	shifted := time.Date(year, month+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	return shifted.Year(), shifted.Month()
}

func lastDayOfMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// CardRecurrenceFromFields reads the two fields out of a card block's Fields map.
// A missing recurrence key yields a nil configuration and no error; a missing
// cardType key yields CardTypeNormal.
//
// The recurrence value is decoded through JSON rather than by type assertion
// because Fields arrives through encoding/json, which turns every number into a
// float64 and every array into a []interface{}. Asserting on int or []int would
// fail on exactly the data the server actually stores.
func CardRecurrenceFromFields(fields map[string]interface{}) (CardType, *RecurrenceConfig, error) {
	cardType := CardTypeNormal

	if raw, ok := fields[CardFieldCardType]; ok && raw != nil {
		name, isString := raw.(string)
		if !isString {
			return CardTypeNormal, nil, ErrInvalidFieldType{field: CardFieldCardType}
		}
		cardType = CardType(name)
	}

	raw, ok := fields[CardFieldRecurrence]
	if !ok || raw == nil {
		return cardType, nil, nil
	}

	encoded, encodeErr := json.Marshal(raw)
	if encodeErr != nil {
		return CardTypeNormal, nil, fmt.Errorf("cannot re-encode the %s field: %w", CardFieldRecurrence, encodeErr)
	}

	cfg := &RecurrenceConfig{}
	if decodeErr := json.Unmarshal(encoded, cfg); decodeErr != nil {
		return CardTypeNormal, nil, fmt.Errorf("cannot decode the %s field: %w", CardFieldRecurrence, decodeErr)
	}

	return cardType, cfg, nil
}

// SetCardRecurrenceFields writes the two fields into a card block's Fields map.
//
// A nil configuration removes the recurrence key rather than storing a null, and
// CardTypeNormal removes the cardType key, so that a card which is not recurring
// carries exactly the fields it carried before this feature existed. The
// configuration is stored as a plain map for the same reason: everything else in
// Fields is JSON-shaped, and the client compares field values by identity.
func SetCardRecurrenceFields(fields map[string]interface{}, cardType CardType, cfg *RecurrenceConfig) error {
	if fields == nil {
		return ErrNilCardFields
	}

	if cardType == CardTypeRecurring {
		fields[CardFieldCardType] = string(CardTypeRecurring)
	} else {
		delete(fields, CardFieldCardType)
	}

	if cfg == nil {
		delete(fields, CardFieldRecurrence)
		return nil
	}

	encoded, encodeErr := json.Marshal(cfg)
	if encodeErr != nil {
		return fmt.Errorf("cannot encode the %s field: %w", CardFieldRecurrence, encodeErr)
	}

	normalised := map[string]interface{}{}
	if decodeErr := json.Unmarshal(encoded, &normalised); decodeErr != nil {
		return fmt.Errorf("cannot normalise the %s field: %w", CardFieldRecurrence, decodeErr)
	}

	fields[CardFieldRecurrence] = normalised

	return nil
}

// RecurringCard is one row of the recurring_cards table, which indexes the cards
// that produce further occurrences of themselves so that the scheduler can ask
// which are due without scanning every block on every board.
//
// The table is a derived index, not a source of truth: the authoritative
// configuration is the card block's fields.recurrence, and every row here can be
// rebuilt from it.
// swagger:model
type RecurringCard struct {
	// The id of the card this row indexes
	// required: true
	CardID string `json:"cardId"`

	// The id of the board the card belongs to
	// required: true
	BoardID string `json:"boardId"`

	// Whether the scheduler should consider this card. This is the conjunction of
	// cardType being "recurring" and recurrence.enabled, as computed by
	// IsRecurrenceActive, and not a mirror of recurrence.enabled on its own.
	// required: true
	Active bool `json:"active"`

	// Denormalised from Config so that the done-column trigger can short-circuit
	// without deserialising it
	// required: true
	Mode RecurrenceMode `json:"mode"`

	// The serialised recurrence configuration
	// required: true
	Config *RecurrenceConfig `json:"config"`

	// When the next occurrence is due, in milliseconds since the epoch. Nil means
	// not scheduled: an "afterDone" card that has not been completed yet.
	// required: false
	NextRunAt *int64 `json:"nextRunAt"`

	// When an occurrence was last produced, in milliseconds since the epoch. Nil
	// means it has never fired.
	// required: false
	LastRunAt *int64 `json:"lastRunAt"`

	// The creation time in milliseconds since the epoch
	// required: false
	CreateAt int64 `json:"createAt"`

	// The last modified time in milliseconds since the epoch
	// required: false
	UpdateAt int64 `json:"updateAt"`
}

// RecurrencePreview is the result of checking a configuration without storing it.
// It carries the whole problem list rather than the first failure, so a settings
// form can show everything that is wrong at once, and the computed next occurrence
// so the same call drives the preview line.
// swagger:model
type RecurrencePreview struct {
	// Whether the configuration may be saved in an enabled state
	// required: true
	Valid bool `json:"valid"`

	// When the recurrence would next fire, in milliseconds since the epoch. Null in
	// "afterDone" mode, which is not scheduled until the card is completed, and null
	// when the configuration is not valid enough to compute one.
	// required: false
	NextRunAt *int64 `json:"nextRunAt"`

	// Every problem found, in a stable order
	// required: false
	Problems []ErrInvalidRecurrence `json:"problems"`
}

// CheckValid returns an error if the row is missing the fields the store needs to
// write it.
//
// This is a structural check only. Whether the configuration itself is coherent,
// and whether it may be enabled, is CheckCardRecurrenceWritable's job at the
// boundary that accepts user input. Enforcing that here would stop the scheduler
// from maintaining a row whose configuration has gone stale, which is exactly
// when the row is most needed.
func (rc *RecurringCard) CheckValid() error {
	if rc == nil {
		return ErrNilRecurringCard
	}
	if rc.CardID == "" {
		return ErrInvalidRecurrence{Field: "cardId", Reason: "is required"}
	}
	if rc.BoardID == "" {
		return ErrInvalidRecurrence{Field: "boardId", Reason: "is required"}
	}
	return nil
}
