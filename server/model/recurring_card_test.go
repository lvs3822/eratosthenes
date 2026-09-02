// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Transition instants these tests are pinned to, all verified against the
// timezone database rather than assumed:
//
//	Europe/Berlin    2026-03-29 01:00 UTC  local 02:00 -> 03:00  (02:00..03:00 missing)
//	Europe/Berlin    2026-10-25 01:00 UTC  local 03:00 -> 02:00  (02:00..03:00 twice)
//	America/New_York 2026-03-08 07:00 UTC  local 02:00 -> 03:00  (02:00..03:00 missing)
//	America/New_York 2026-11-01 06:00 UTC  local 02:00 -> 01:00  (01:00..02:00 twice)
//
// Europe/Moscow is deliberately used for the non-DST cases: it has not observed
// daylight saving since 2014, so it can never mask a bug in the transition code.
const (
	zoneBerlin  = "Europe/Berlin"
	zoneNewYork = "America/New_York"
	zoneMoscow  = "Europe/Moscow"
)

func utcMillis(year int, month time.Month, day, hour, minute int) int64 {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC).UnixMilli()
}

func atZone(t *testing.T, zone string, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()

	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)

	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

// describe renders a result as UTC and as local wall clock, so that a failure
// message says what actually happened instead of quoting two epoch numbers.
func describe(t *testing.T, zone string, millis int64) string {
	t.Helper()

	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)

	instant := time.UnixMilli(millis)

	return instant.UTC().Format("2006-01-02 15:04:05 MST") + " / " + instant.In(loc).Format("2006-01-02 15:04:05 MST -0700")
}

func scheduleConfig(zone, clock string, startAt time.Time, rule *RecurrenceRule) *RecurrenceConfig {
	return &RecurrenceConfig{
		Enabled:         true,
		Mode:            RecurrenceModeSchedule,
		GroupPropertyID: "group-property-id",
		TargetOptionID:  "target-option-id",
		Timezone:        zone,
		Time:            clock,
		HistoryMode:     RecurrenceHistoryNewInstance,
		StartAt:         startAt.UnixMilli(),
		Rule:            rule,
	}
}

func afterDoneConfig(zone, clock string, delayDays int) *RecurrenceConfig {
	return &RecurrenceConfig{
		Enabled:         true,
		Mode:            RecurrenceModeAfterDone,
		GroupPropertyID: "group-property-id",
		TargetOptionID:  "target-option-id",
		Timezone:        zone,
		Time:            clock,
		HistoryMode:     RecurrenceHistoryNewInstance,
		DoneOptionID:    "done-option-id",
		DelayDays:       delayDays,
	}
}

func TestNextRunAtDaylightSaving(t *testing.T) {
	t.Run("spring forward in Europe/Berlin fires at the instant the clock reached 02:30", func(t *testing.T) {
		// 02:30 does not exist on 2026-03-29. time.Date answers 03:30 CEST here,
		// which is a whole hour late.
		cfg := scheduleConfig(zoneBerlin, "02:30", atZone(t, zoneBerlin, 2026, 3, 1, 2, 30), &RecurrenceRule{
			Kind:     RecurrenceRuleDaily,
			Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 3, 28, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 3, 29, 1, 0), got, describe(t, zoneBerlin, got))
	})

	t.Run("spring forward in America/New_York never fires before the requested time", func(t *testing.T) {
		// The regression that matters most: for a missing 02:30 time.Date answers
		// 01:30 EST, which is BEFORE the time the user asked for.
		cfg := scheduleConfig(zoneNewYork, "02:30", atZone(t, zoneNewYork, 2026, 3, 1, 2, 30), &RecurrenceRule{
			Kind:     RecurrenceRuleDaily,
			Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneNewYork, 2026, 3, 7, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 3, 8, 7, 0), got, describe(t, zoneNewYork, got))

		naive := time.Date(2026, 3, 8, 2, 30, 0, 0, time.UnixMilli(got).Location()).UnixMilli()
		assert.Greater(t, got, naive, "an occurrence must never land before its nominal wall clock")
	})

	t.Run("fall back in Europe/Berlin takes the first pass of a repeated 02:30", func(t *testing.T) {
		// 02:30 happens twice on 2026-10-25, at 00:30 UTC as CEST and at 01:30 UTC
		// as CET. time.Date answers the second one here.
		cfg := scheduleConfig(zoneBerlin, "02:30", atZone(t, zoneBerlin, 2026, 10, 1, 2, 30), &RecurrenceRule{
			Kind:     RecurrenceRuleDaily,
			Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 10, 24, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 10, 25, 0, 30), got, describe(t, zoneBerlin, got))
	})

	t.Run("fall back in America/New_York takes the first pass of a repeated 01:30", func(t *testing.T) {
		cfg := scheduleConfig(zoneNewYork, "01:30", atZone(t, zoneNewYork, 2026, 10, 1, 1, 30), &RecurrenceRule{
			Kind:     RecurrenceRuleDaily,
			Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneNewYork, 2026, 10, 31, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 11, 1, 5, 30), got, describe(t, zoneNewYork, got))
	})

	t.Run("a repeated wall clock fires exactly once, not on both passes", func(t *testing.T) {
		cfg := scheduleConfig(zoneBerlin, "02:30", atZone(t, zoneBerlin, 2026, 10, 1, 2, 30), &RecurrenceRule{
			Kind:     RecurrenceRuleDaily,
			Interval: 1,
		})

		first, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 10, 24, 12, 0))
		require.NoError(t, err)
		require.Equal(t, utcMillis(2026, 10, 25, 0, 30), first)

		// Feeding the first pass back in must skip the second pass of the same
		// reading, an hour later, and move on to the next day.
		second, err := NextRunAt(cfg, time.UnixMilli(first))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 10, 26, 1, 30), second, describe(t, zoneBerlin, second))
	})

	t.Run("a daily 09:00 rule stays at 09:00 local across a transition", func(t *testing.T) {
		cfg := scheduleConfig(zoneBerlin, "09:00", atZone(t, zoneBerlin, 2026, 3, 1, 9, 0), &RecurrenceRule{
			Kind:     RecurrenceRuleDaily,
			Interval: 1,
		})

		before, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 3, 27, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 3, 28, 8, 0), before, describe(t, zoneBerlin, before))

		after, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 3, 29, 12, 0))
		require.NoError(t, err)
		// One hour earlier in UTC, same 09:00 on the wall clock.
		assert.Equal(t, utcMillis(2026, 3, 30, 7, 0), after, describe(t, zoneBerlin, after))
	})
}

func TestNextRunAtMonthly(t *testing.T) {
	on31st := func(startAt time.Time) *RecurrenceConfig {
		return scheduleConfig(zoneMoscow, "09:00", startAt, &RecurrenceRule{
			Kind:       RecurrenceRuleMonthly,
			Interval:   1,
			DayOfMonth: 31,
		})
	}

	t.Run("clamps to the last day of a month that is too short", func(t *testing.T) {
		cfg := on31st(atZone(t, zoneMoscow, 2026, 1, 1, 9, 0))

		// Moscow is UTC+3 all year, so 09:00 local is 06:00 UTC throughout.
		cases := []struct {
			name string
			from time.Time
			want int64
		}{
			{"January has a 31st", atZone(t, zoneMoscow, 2026, 1, 15, 0, 0), utcMillis(2026, 1, 31, 6, 0)},
			{"February clamps to the 28th", atZone(t, zoneMoscow, 2026, 2, 1, 0, 0), utcMillis(2026, 2, 28, 6, 0)},
			{"March returns to the 31st", atZone(t, zoneMoscow, 2026, 3, 1, 0, 0), utcMillis(2026, 3, 31, 6, 0)},
			{"April clamps to the 30th", atZone(t, zoneMoscow, 2026, 4, 1, 0, 0), utcMillis(2026, 4, 30, 6, 0)},
			{"May returns to the 31st", atZone(t, zoneMoscow, 2026, 5, 1, 0, 0), utcMillis(2026, 5, 31, 6, 0)},
			{"June clamps to the 30th", atZone(t, zoneMoscow, 2026, 6, 1, 0, 0), utcMillis(2026, 6, 30, 6, 0)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := NextRunAt(cfg, tc.from)
				require.NoError(t, err)
				assert.Equal(t, tc.want, got, describe(t, zoneMoscow, got))
			})
		}
	})

	t.Run("clamps to the 29th in a leap February", func(t *testing.T) {
		cfg := on31st(atZone(t, zoneMoscow, 2028, 1, 1, 9, 0))

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2028, 2, 1, 0, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2028, 2, 29, 6, 0), got, describe(t, zoneMoscow, got))
	})

	t.Run("clamping is not sticky: the month after February is the 31st again", func(t *testing.T) {
		cfg := on31st(atZone(t, zoneMoscow, 2026, 1, 1, 9, 0))

		february, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 2, 1, 0, 0))
		require.NoError(t, err)
		require.Equal(t, utcMillis(2026, 2, 28, 6, 0), february)

		march, err := NextRunAt(cfg, time.UnixMilli(february))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 3, 31, 6, 0), march, "clamping must not rewrite dayOfMonth")
	})

	t.Run("does not overflow into the next month, which is what time.Date would do", func(t *testing.T) {
		// time.Date(2026, February, 31, ...) normalises to 3 March. Landing there
		// would silently move a monthly rule three days into the wrong month.
		cfg := on31st(atZone(t, zoneMoscow, 2026, 1, 1, 9, 0))

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 2, 1, 0, 0))
		require.NoError(t, err)

		loc, err := time.LoadLocation(zoneMoscow)
		require.NoError(t, err)
		assert.Equal(t, time.February, time.UnixMilli(got).In(loc).Month())
		assert.NotEqual(t, utcMillis(2026, 3, 3, 6, 0), got)
	})

	t.Run("honours an interval greater than one from the anchor", func(t *testing.T) {
		cfg := scheduleConfig(zoneMoscow, "09:00", atZone(t, zoneMoscow, 2026, 1, 10, 12, 0), &RecurrenceRule{
			Kind:       RecurrenceRuleMonthly,
			Interval:   3,
			DayOfMonth: 15,
		})

		first, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 1, 10, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 1, 15, 6, 0), first, describe(t, zoneMoscow, first))

		second, err := NextRunAt(cfg, time.UnixMilli(first))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 4, 15, 6, 0), second, describe(t, zoneMoscow, second))

		third, err := NextRunAt(cfg, time.UnixMilli(second))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 7, 15, 6, 0), third, describe(t, zoneMoscow, third))
	})
}

func TestNextRunAtWeekly(t *testing.T) {
	t.Run("walks several weekdays in order", func(t *testing.T) {
		// 2026-01-05 is a Monday.
		cfg := scheduleConfig(zoneMoscow, "09:00", atZone(t, zoneMoscow, 2026, 1, 5, 0, 0), &RecurrenceRule{
			Kind:     RecurrenceRuleWeekly,
			Interval: 1,
			Weekdays: []int{5, 1, 3}, // deliberately unsorted: Friday, Monday, Wednesday
		})

		want := []int64{
			utcMillis(2026, 1, 5, 6, 0),  // Monday
			utcMillis(2026, 1, 7, 6, 0),  // Wednesday
			utcMillis(2026, 1, 9, 6, 0),  // Friday
			utcMillis(2026, 1, 12, 6, 0), // Monday again
			utcMillis(2026, 1, 14, 6, 0), // Wednesday
		}

		from := atZone(t, zoneMoscow, 2026, 1, 4, 12, 0) // Sunday
		for i, expected := range want {
			got, err := NextRunAt(cfg, from)
			require.NoError(t, err, "step %d", i)
			require.Equal(t, expected, got, "step %d: %s", i, describe(t, zoneMoscow, got))
			from = time.UnixMilli(got)
		}
	})

	t.Run("skips the off weeks of an interval of two", func(t *testing.T) {
		cfg := scheduleConfig(zoneMoscow, "09:00", atZone(t, zoneMoscow, 2026, 1, 5, 0, 0), &RecurrenceRule{
			Kind:     RecurrenceRuleWeekly,
			Interval: 2,
			Weekdays: []int{1, 3},
		})

		want := []int64{
			utcMillis(2026, 1, 5, 6, 0),  // Monday, anchor week
			utcMillis(2026, 1, 7, 6, 0),  // Wednesday, anchor week
			utcMillis(2026, 1, 19, 6, 0), // the week of 12 January is skipped
			utcMillis(2026, 1, 21, 6, 0),
			utcMillis(2026, 2, 2, 6, 0),
		}

		from := atZone(t, zoneMoscow, 2026, 1, 4, 12, 0)
		for i, expected := range want {
			got, err := NextRunAt(cfg, from)
			require.NoError(t, err, "step %d", i)
			require.Equal(t, expected, got, "step %d: %s", i, describe(t, zoneMoscow, got))
			from = time.UnixMilli(got)
		}
	})

	t.Run("keeps its phase when the weekdays are edited", func(t *testing.T) {
		// startAt is preserved across edits, so adding a weekday must not move the
		// cycle onto the other set of weeks.
		anchor := atZone(t, zoneMoscow, 2026, 1, 5, 0, 0)
		before := scheduleConfig(zoneMoscow, "09:00", anchor, &RecurrenceRule{
			Kind: RecurrenceRuleWeekly, Interval: 2, Weekdays: []int{1},
		})
		after := scheduleConfig(zoneMoscow, "09:00", anchor, &RecurrenceRule{
			Kind: RecurrenceRuleWeekly, Interval: 2, Weekdays: []int{1, 4},
		})

		from := atZone(t, zoneMoscow, 2026, 1, 20, 12, 0)

		beforeNext, err := NextRunAt(before, from)
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 2, 2, 6, 0), beforeNext, describe(t, zoneMoscow, beforeNext))

		afterNext, err := NextRunAt(after, from)
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 1, 22, 6, 0), afterNext, describe(t, zoneMoscow, afterNext))
	})

	t.Run("crosses a spring forward without losing a week", func(t *testing.T) {
		// 2026-03-29 is the Sunday Berlin springs forward.
		cfg := scheduleConfig(zoneBerlin, "09:00", atZone(t, zoneBerlin, 2026, 3, 2, 0, 0), &RecurrenceRule{
			Kind: RecurrenceRuleWeekly, Interval: 1, Weekdays: []int{7},
		})

		got, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 3, 23, 12, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 3, 29, 7, 0), got, describe(t, zoneBerlin, got))
	})
}

func TestNextRunAtDaily(t *testing.T) {
	t.Run("returns today's occurrence when it is still ahead", func(t *testing.T) {
		cfg := scheduleConfig(zoneMoscow, "18:00", atZone(t, zoneMoscow, 2026, 5, 1, 0, 0), &RecurrenceRule{
			Kind: RecurrenceRuleDaily, Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 5, 10, 9, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 5, 10, 15, 0), got, describe(t, zoneMoscow, got))
	})

	t.Run("rolls to tomorrow once today's occurrence has passed", func(t *testing.T) {
		cfg := scheduleConfig(zoneMoscow, "18:00", atZone(t, zoneMoscow, 2026, 5, 1, 0, 0), &RecurrenceRule{
			Kind: RecurrenceRuleDaily, Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 5, 10, 21, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 5, 11, 15, 0), got, describe(t, zoneMoscow, got))
	})

	t.Run("never produces an occurrence before the recurrence was enabled", func(t *testing.T) {
		// Enabled at noon on 10 May for a 09:00 daily rule: the 09:00 that already
		// happened that morning is not an occurrence.
		cfg := scheduleConfig(zoneMoscow, "09:00", atZone(t, zoneMoscow, 2026, 5, 10, 12, 0), &RecurrenceRule{
			Kind: RecurrenceRuleDaily, Interval: 1,
		})

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 5, 1, 0, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 5, 11, 6, 0), got, describe(t, zoneMoscow, got))
	})

	t.Run("honours an interval greater than one from the anchor", func(t *testing.T) {
		cfg := scheduleConfig(zoneMoscow, "09:00", atZone(t, zoneMoscow, 2026, 5, 1, 0, 0), &RecurrenceRule{
			Kind: RecurrenceRuleDaily, Interval: 3,
		})

		want := []int64{
			utcMillis(2026, 5, 1, 6, 0),
			utcMillis(2026, 5, 4, 6, 0),
			utcMillis(2026, 5, 7, 6, 0),
			utcMillis(2026, 5, 10, 6, 0),
		}

		from := atZone(t, zoneMoscow, 2026, 4, 30, 12, 0)
		for i, expected := range want {
			got, err := NextRunAt(cfg, from)
			require.NoError(t, err, "step %d", i)
			require.Equal(t, expected, got, "step %d: %s", i, describe(t, zoneMoscow, got))
			from = time.UnixMilli(got)
		}
	})
}

func TestNextRunAtAfterDone(t *testing.T) {
	t.Run("lands at the configured time of day, not at the completion time", func(t *testing.T) {
		cfg := afterDoneConfig(zoneMoscow, "09:00", 3)

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 5, 10, 15, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 5, 13, 6, 0), got, describe(t, zoneMoscow, got))
	})

	t.Run("counts days in the configured timezone, not in UTC", func(t *testing.T) {
		// 22:00 in Moscow is still the previous day in UTC. Counting in UTC would
		// answer one day early.
		cfg := afterDoneConfig(zoneMoscow, "09:00", 1)

		got, err := NextRunAt(cfg, atZone(t, zoneMoscow, 2026, 5, 10, 22, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 5, 11, 6, 0), got, describe(t, zoneMoscow, got))
	})

	t.Run("crosses a spring forward", func(t *testing.T) {
		cfg := afterDoneConfig(zoneBerlin, "02:30", 1)

		got, err := NextRunAt(cfg, atZone(t, zoneBerlin, 2026, 3, 28, 20, 0))
		require.NoError(t, err)
		assert.Equal(t, utcMillis(2026, 3, 29, 1, 0), got, describe(t, zoneBerlin, got))
	})

	t.Run("is strictly after completion even at the very end of a day", func(t *testing.T) {
		cfg := afterDoneConfig(zoneMoscow, "00:00", 1)
		from := atZone(t, zoneMoscow, 2026, 5, 10, 23, 59)

		got, err := NextRunAt(cfg, from)
		require.NoError(t, err)
		assert.Greater(t, got, from.UnixMilli())
		assert.Equal(t, utcMillis(2026, 5, 10, 21, 0), got, describe(t, zoneMoscow, got))
	})
}

func TestNextRunAtContract(t *testing.T) {
	anchor := atZone(t, zoneBerlin, 2026, 1, 5, 8, 30)

	configs := map[string]*RecurrenceConfig{
		"daily": scheduleConfig(zoneBerlin, "09:00", anchor, &RecurrenceRule{
			Kind: RecurrenceRuleDaily, Interval: 2,
		}),
		"weekly": scheduleConfig(zoneBerlin, "02:30", anchor, &RecurrenceRule{
			Kind: RecurrenceRuleWeekly, Interval: 1, Weekdays: []int{1, 3, 6, 7},
		}),
		"monthly": scheduleConfig(zoneBerlin, "02:30", anchor, &RecurrenceRule{
			Kind: RecurrenceRuleMonthly, Interval: 1, DayOfMonth: 31,
		}),
		"afterDone": afterDoneConfig(zoneBerlin, "02:30", 1),
	}

	for name, cfg := range configs {
		t.Run(name+" advances strictly and lands on a whole minute", func(t *testing.T) {
			// Walking a whole year covers both transitions and every short month.
			cursor := atZone(t, zoneBerlin, 2026, 1, 1, 0, 0)

			for i := 0; i < 400; i++ {
				got, err := NextRunAt(cfg, cursor)
				require.NoError(t, err, "step %d", i)

				next := time.UnixMilli(got)
				require.True(t, next.After(cursor),
					"step %d: %s is not after %s", i, describe(t, zoneBerlin, got), cursor)
				require.Zero(t, next.Second(), "step %d: %s", i, describe(t, zoneBerlin, got))
				require.Zero(t, got%int64(time.Second/time.Millisecond), "step %d", i)

				cursor = next
			}

			assert.Greater(t, cursor.Year(), 2025)
		})
	}
}

func TestNextRunAtRejectsBadInput(t *testing.T) {
	t.Run("a nil configuration", func(t *testing.T) {
		_, err := NextRunAt(nil, time.Now())
		require.ErrorIs(t, err, ErrNilRecurrence)
	})

	t.Run("an invalid configuration, whatever enabled is set to", func(t *testing.T) {
		cfg := scheduleConfig(zoneMoscow, "09:00", time.Now(), nil)
		cfg.Enabled = false

		_, err := NextRunAt(cfg, time.Now())
		require.Error(t, err)

		var invalid ErrRecurrenceInvalid
		require.ErrorAs(t, err, &invalid)
		assert.Equal(t, RecurrenceFieldRule, invalid.Problems[0].Field)
	})
}

func TestRecurrenceConfigProblems(t *testing.T) {
	valid := func() *RecurrenceConfig {
		return scheduleConfig(zoneMoscow, "09:00", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), &RecurrenceRule{
			Kind: RecurrenceRuleDaily, Interval: 1,
		})
	}

	t.Run("a complete schedule configuration has no problems", func(t *testing.T) {
		require.NoError(t, valid().CheckValid())
	})

	t.Run("a complete afterDone configuration has no problems", func(t *testing.T) {
		require.NoError(t, afterDoneConfig(zoneMoscow, "09:00", 1).CheckValid())
	})

	cases := []struct {
		name   string
		mutate func(cfg *RecurrenceConfig)
		field  string
	}{
		{"unknown mode", func(c *RecurrenceConfig) { c.Mode = "whenever" }, RecurrenceFieldMode},
		{"missing group property", func(c *RecurrenceConfig) { c.GroupPropertyID = "" }, RecurrenceFieldGroupProperty},
		{"missing target option", func(c *RecurrenceConfig) { c.TargetOptionID = "" }, RecurrenceFieldTargetOption},
		{"missing timezone", func(c *RecurrenceConfig) { c.Timezone = "" }, RecurrenceFieldTimezone},
		{"unknown timezone", func(c *RecurrenceConfig) { c.Timezone = "Middle/Earth" }, RecurrenceFieldTimezone},
		{"host local timezone", func(c *RecurrenceConfig) { c.Timezone = "Local" }, RecurrenceFieldTimezone},
		{"abbreviated timezone", func(c *RecurrenceConfig) { c.Timezone = "MSK" }, RecurrenceFieldTimezone},
		{"missing time", func(c *RecurrenceConfig) { c.Time = "" }, RecurrenceFieldTime},
		{"unpadded time", func(c *RecurrenceConfig) { c.Time = "9:00" }, RecurrenceFieldTime},
		{"time with seconds", func(c *RecurrenceConfig) { c.Time = "09:00:00" }, RecurrenceFieldTime},
		{"hour out of range", func(c *RecurrenceConfig) { c.Time = "24:00" }, RecurrenceFieldTime},
		{"minute out of range", func(c *RecurrenceConfig) { c.Time = "09:60" }, RecurrenceFieldTime},
		{"unknown history mode", func(c *RecurrenceConfig) { c.HistoryMode = "keepBoth" }, RecurrenceFieldHistoryMode},
		{"missing anchor", func(c *RecurrenceConfig) { c.StartAt = 0 }, RecurrenceFieldStartAt},
		{"negative anchor", func(c *RecurrenceConfig) { c.StartAt = -1 }, RecurrenceFieldStartAt},
		{"schedule mode without a rule", func(c *RecurrenceConfig) { c.Rule = nil }, RecurrenceFieldRule},
		{"unknown rule kind", func(c *RecurrenceConfig) { c.Rule.Kind = "fortnightly" }, RecurrenceFieldRuleKind},
		{"zero interval", func(c *RecurrenceConfig) { c.Rule.Interval = 0 }, RecurrenceFieldInterval},
		{"negative interval", func(c *RecurrenceConfig) { c.Rule.Interval = -3 }, RecurrenceFieldInterval},
		{"schedule mode with a done option", func(c *RecurrenceConfig) { c.DoneOptionID = "x" }, RecurrenceFieldDoneOption},
		{"schedule mode with a delay", func(c *RecurrenceConfig) { c.DelayDays = 2 }, RecurrenceFieldDelayDays},
		{"weekdays on a daily rule", func(c *RecurrenceConfig) { c.Rule.Weekdays = []int{1} }, RecurrenceFieldWeekdays},
		{"day of month on a daily rule", func(c *RecurrenceConfig) { c.Rule.DayOfMonth = 5 }, RecurrenceFieldDayOfMonth},
		{"weekly without weekdays", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleWeekly
		}, RecurrenceFieldWeekdays},
		{"weekly with an empty weekday list", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleWeekly
			c.Rule.Weekdays = []int{}
		}, RecurrenceFieldWeekdays},
		{"weekday zero", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleWeekly
			c.Rule.Weekdays = []int{0, 1}
		}, RecurrenceFieldWeekdays},
		{"weekday eight", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleWeekly
			c.Rule.Weekdays = []int{8}
		}, RecurrenceFieldWeekdays},
		{"duplicated weekday", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleWeekly
			c.Rule.Weekdays = []int{2, 2}
		}, RecurrenceFieldWeekdays},
		{"monthly without a day of month", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleMonthly
		}, RecurrenceFieldDayOfMonth},
		{"day of month thirty two", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleMonthly
			c.Rule.DayOfMonth = 32
		}, RecurrenceFieldDayOfMonth},
		{"reserved last day of month", func(c *RecurrenceConfig) {
			c.Rule.Kind = RecurrenceRuleMonthly
			c.Rule.DayOfMonth = RecurrenceDayOfMonthLastDay
		}, RecurrenceFieldDayOfMonth},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid()
			tc.mutate(cfg)

			problems := cfg.Problems()
			require.NotEmpty(t, problems)

			fields := make([]string, 0, len(problems))
			for _, p := range problems {
				fields = append(fields, p.Field)
				assert.NotEmpty(t, p.Reason, "every problem must explain itself")
			}
			assert.Contains(t, fields, tc.field)
		})
	}

	afterDoneCases := []struct {
		name   string
		mutate func(cfg *RecurrenceConfig)
		field  string
	}{
		{"missing done option", func(c *RecurrenceConfig) { c.DoneOptionID = "" }, RecurrenceFieldDoneOption},
		{"zero delay", func(c *RecurrenceConfig) { c.DelayDays = 0 }, RecurrenceFieldDelayDays},
		{"negative delay", func(c *RecurrenceConfig) { c.DelayDays = -1 }, RecurrenceFieldDelayDays},
		{"afterDone mode with a rule", func(c *RecurrenceConfig) {
			c.Rule = &RecurrenceRule{Kind: RecurrenceRuleDaily, Interval: 1}
		}, RecurrenceFieldRule},
	}

	for _, tc := range afterDoneCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := afterDoneConfig(zoneMoscow, "09:00", 1)
			tc.mutate(cfg)

			problems := cfg.Problems()
			require.NotEmpty(t, problems)

			fields := make([]string, 0, len(problems))
			for _, p := range problems {
				fields = append(fields, p.Field)
			}
			assert.Contains(t, fields, tc.field)
		})
	}

	t.Run("reports every problem at once rather than the first", func(t *testing.T) {
		cfg := &RecurrenceConfig{Mode: RecurrenceModeSchedule}

		problems := cfg.Problems()
		assert.GreaterOrEqual(t, len(problems), 6)

		joined := cfg.CheckValid()
		require.Error(t, joined)
		assert.Contains(t, joined.Error(), RecurrenceFieldGroupProperty)
		assert.Contains(t, joined.Error(), RecurrenceFieldRule)
	})

	t.Run("a nil configuration has no problems of its own", func(t *testing.T) {
		var cfg *RecurrenceConfig
		assert.Empty(t, cfg.Problems())
		assert.NoError(t, cfg.CheckValid())
	})
}

func TestIsRecurrenceActive(t *testing.T) {
	enabled := afterDoneConfig(zoneMoscow, "09:00", 1)
	paused := afterDoneConfig(zoneMoscow, "09:00", 1)
	paused.Enabled = false

	cases := []struct {
		name     string
		cardType CardType
		cfg      *RecurrenceConfig
		want     bool
	}{
		{"recurring and enabled", CardTypeRecurring, enabled, true},
		{"recurring but paused", CardTypeRecurring, paused, false},
		{"recurring with no configuration", CardTypeRecurring, nil, false},
		{"normal with a leftover enabled configuration", CardTypeNormal, enabled, false},
		{"absent card type with a leftover configuration", "", enabled, false},
		{"normal with nothing", CardTypeNormal, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsRecurrenceActive(tc.cardType, tc.cfg))
		})
	}
}

func TestCheckCardRecurrenceWritable(t *testing.T) {
	complete := func() *RecurrenceConfig { return afterDoneConfig(zoneMoscow, "09:00", 1) }

	incomplete := func() *RecurrenceConfig {
		cfg := complete()
		cfg.DoneOptionID = ""
		cfg.TargetOptionID = ""
		return cfg
	}

	t.Run("a valid enabled recurring card may be written", func(t *testing.T) {
		require.NoError(t, CheckCardRecurrenceWritable(CardTypeRecurring, complete()))
	})

	t.Run("an incomplete but paused card may be written", func(t *testing.T) {
		cfg := incomplete()
		cfg.Enabled = false

		require.NoError(t, CheckCardRecurrenceWritable(CardTypeRecurring, cfg),
			"a work in progress must be storable while it is switched off")
		require.NotEmpty(t, cfg.Problems(), "but it is still reported as incomplete")
	})

	t.Run("an incomplete enabled card is rejected", func(t *testing.T) {
		err := CheckCardRecurrenceWritable(CardTypeRecurring, incomplete())
		require.Error(t, err)

		var invalid ErrRecurrenceInvalid
		require.ErrorAs(t, err, &invalid)
		assert.Len(t, invalid.Problems, 2)
	})

	t.Run("a normal card with recurrence enabled is rejected", func(t *testing.T) {
		err := CheckCardRecurrenceWritable(CardTypeNormal, complete())
		require.Error(t, err)
		assert.Contains(t, err.Error(), CardFieldCardType)
	})

	t.Run("a normal card with a paused leftover configuration may be written", func(t *testing.T) {
		cfg := incomplete()
		cfg.Enabled = false

		require.NoError(t, CheckCardRecurrenceWritable(CardTypeNormal, cfg),
			"switching the type back to normal must not discard the settings")
	})

	t.Run("a recurring card with no configuration at all is rejected only once enabled", func(t *testing.T) {
		require.NoError(t, CheckCardRecurrenceWritable(CardTypeRecurring, nil))

		problems := CardRecurrenceProblems(CardTypeRecurring, nil)
		require.Len(t, problems, 1)
		assert.Equal(t, CardFieldRecurrence, problems[0].Field)
	})

	t.Run("an unknown card type is reported", func(t *testing.T) {
		problems := CardRecurrenceProblems("weekly-ish", complete())
		require.NotEmpty(t, problems)
		assert.Equal(t, CardFieldCardType, problems[0].Field)
	})
}

func TestCardRecurrenceFields(t *testing.T) {
	t.Run("decodes a configuration that arrived through encoding/json", func(t *testing.T) {
		// The case that type assertions get wrong: every number in Fields is a
		// float64 and every array a []interface{} once it has been through JSON.
		var fields map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(sampleRecurrenceFieldsJSON), &fields))

		require.IsType(t, float64(0), fields[CardFieldRecurrence].(map[string]interface{})["startAt"])

		cardType, cfg, err := CardRecurrenceFromFields(fields)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, CardTypeRecurring, cardType)
		assert.True(t, cfg.Enabled)
		assert.Equal(t, RecurrenceModeSchedule, cfg.Mode)
		assert.Equal(t, "Europe/Moscow", cfg.Timezone)
		assert.Equal(t, "09:30", cfg.Time)
		assert.Equal(t, RecurrenceHistoryNewInstance, cfg.HistoryMode)
		assert.Equal(t, int64(1767225600000), cfg.StartAt)

		require.NotNil(t, cfg.Rule)
		assert.Equal(t, RecurrenceRuleWeekly, cfg.Rule.Kind)
		assert.Equal(t, 2, cfg.Rule.Interval)
		assert.Equal(t, []int{1, 3, 5}, cfg.Rule.Weekdays)

		require.NoError(t, cfg.CheckValid())
	})

	t.Run("a card with neither field is a normal card", func(t *testing.T) {
		cardType, cfg, err := CardRecurrenceFromFields(map[string]interface{}{"icon": "🎨"})
		require.NoError(t, err)
		assert.Equal(t, CardTypeNormal, cardType)
		assert.Nil(t, cfg)
	})

	t.Run("a card type that is not a string is an error", func(t *testing.T) {
		_, _, err := CardRecurrenceFromFields(map[string]interface{}{CardFieldCardType: 7})
		require.Error(t, err)

		var wrongType ErrInvalidFieldType
		require.ErrorAs(t, err, &wrongType)
	})

	t.Run("round trips through the fields map", func(t *testing.T) {
		original := afterDoneConfig(zoneMoscow, "09:00", 4)
		fields := map[string]interface{}{"icon": "🎨"}

		require.NoError(t, SetCardRecurrenceFields(fields, CardTypeRecurring, original))

		// Through the wire and back, as a real request would go.
		encoded, err := json.Marshal(fields)
		require.NoError(t, err)
		var decoded map[string]interface{}
		require.NoError(t, json.Unmarshal(encoded, &decoded))

		cardType, cfg, err := CardRecurrenceFromFields(decoded)
		require.NoError(t, err)
		assert.Equal(t, CardTypeRecurring, cardType)
		assert.Equal(t, original, cfg)
		assert.Equal(t, "🎨", decoded["icon"], "unrelated fields must survive untouched")
	})

	t.Run("a normal card carries neither key", func(t *testing.T) {
		fields := map[string]interface{}{
			"icon":              "🎨",
			CardFieldCardType:   string(CardTypeRecurring),
			CardFieldRecurrence: map[string]interface{}{"enabled": true},
		}

		require.NoError(t, SetCardRecurrenceFields(fields, CardTypeNormal, nil))

		assert.NotContains(t, fields, CardFieldCardType)
		assert.NotContains(t, fields, CardFieldRecurrence)
		assert.Equal(t, "🎨", fields["icon"])
	})

	t.Run("a nil fields map is an error", func(t *testing.T) {
		require.ErrorIs(t, SetCardRecurrenceFields(nil, CardTypeNormal, nil), ErrNilCardFields)
	})

	t.Run("the stored value is plain JSON data, not a struct", func(t *testing.T) {
		fields := map[string]interface{}{}
		require.NoError(t, SetCardRecurrenceFields(fields, CardTypeRecurring, afterDoneConfig(zoneMoscow, "09:00", 1)))

		stored, ok := fields[CardFieldRecurrence].(map[string]interface{})
		require.True(t, ok, "everything else in Fields is JSON shaped and this must be too")
		assert.NotContains(t, stored, "rule", "an absent rule must not be stored as a null")
	})
}

const sampleRecurrenceFieldsJSON = `
{
	"icon":"🎨",
	"isTemplate":false,
	"contentOrder":[],
	"properties":{},
	"cardType":"recurring",
	"recurrence":{
		"enabled":true,
		"mode":"schedule",
		"groupPropertyId":"af6fcbb8-ca56-4b73-83eb-37437b9a667d",
		"targetOptionId":"77c539af-309c-4db1-8329-d20ef7e9eacd",
		"timezone":"Europe/Moscow",
		"time":"09:30",
		"historyMode":"newInstance",
		"startAt":1767225600000,
		"rule":{
			"kind":"weekly",
			"interval":2,
			"weekdays":[1,3,5]
		}
	}
}`
