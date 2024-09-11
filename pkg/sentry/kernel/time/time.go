// Copyright 2018 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package time defines the Timer type, which provides a periodic timer that
// works by sampling a user-provided clock.
package time

import (
	"fmt"
	"math"
	"time"

	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/sync"
	"gvisor.dev/gvisor/pkg/waiter"
)

// Events that may be generated by a Clock.
const (
	// ClockEventSet occurs when a Clock undergoes a discontinuous change.
	ClockEventSet waiter.EventMask = 1 << iota

	// ClockEventRateIncrease occurs when the rate at which a Clock advances
	// increases significantly, such that values returned by previous calls to
	// Clock.WallTimeUntil may be too large.
	ClockEventRateIncrease
)

// Time represents an instant in time with nanosecond precision.
//
// Time may represent time with respect to any clock and may not have any
// meaning in the real world.
//
// +stateify savable
type Time struct {
	ns int64
}

var (
	// MinTime is the zero time instant, the lowest possible time that can
	// be represented by Time.
	MinTime = Time{ns: math.MinInt64}

	// MaxTime is the highest possible time that can be represented by
	// Time.
	MaxTime = Time{ns: math.MaxInt64}

	// ZeroTime represents the zero time in an unspecified Clock's domain.
	ZeroTime = Time{ns: 0}
)

const (
	// MinDuration is the minimum duration representable by time.Duration.
	MinDuration = time.Duration(math.MinInt64)

	// MaxDuration is the maximum duration representable by time.Duration.
	MaxDuration = time.Duration(math.MaxInt64)
)

// FromNanoseconds returns a Time representing the point ns nanoseconds after
// an unspecified Clock's zero time.
func FromNanoseconds(ns int64) Time {
	return Time{ns}
}

// FromSeconds returns a Time representing the point s seconds after an
// unspecified Clock's zero time.
func FromSeconds(s int64) Time {
	if s > math.MaxInt64/time.Second.Nanoseconds() {
		return MaxTime
	}
	return Time{s * 1e9}
}

// FromUnix converts from Unix seconds and nanoseconds to Time, assuming a real
// time Unix clock domain.
func FromUnix(s int64, ns int64) Time {
	if s > math.MaxInt64/time.Second.Nanoseconds() {
		return MaxTime
	}
	t := s * 1e9
	if t > math.MaxInt64-ns {
		return MaxTime
	}
	return Time{t + ns}
}

// FromTimespec converts from Linux Timespec to Time.
func FromTimespec(ts linux.Timespec) Time {
	return Time{ts.ToNsecCapped()}
}

// FromTimeval converts a Linux Timeval to Time.
func FromTimeval(tv linux.Timeval) Time {
	return Time{tv.ToNsecCapped()}
}

// Nanoseconds returns nanoseconds elapsed since the zero time in t's Clock
// domain. If t represents walltime, this is nanoseconds since the Unix epoch.
func (t Time) Nanoseconds() int64 {
	return t.ns
}

// Microseconds returns microseconds elapsed since the zero time in t's Clock
// domain. If t represents walltime, this is microseconds since the Unix epoch.
func (t Time) Microseconds() int64 {
	return t.ns / 1000
}

// Seconds returns seconds elapsed since the zero time in t's Clock domain. If
// t represents walltime, this is seconds since Unix epoch.
func (t Time) Seconds() int64 {
	return t.Nanoseconds() / time.Second.Nanoseconds()
}

// Timespec converts Time to a Linux timespec.
func (t Time) Timespec() linux.Timespec {
	return linux.NsecToTimespec(t.Nanoseconds())
}

// Unix returns the (seconds, nanoseconds) representation of t such that
// seconds*1e9 + nanoseconds = t.
func (t Time) Unix() (s int64, ns int64) {
	s = t.ns / 1e9
	ns = t.ns % 1e9
	return
}

// TimeT converts Time to a Linux time_t.
func (t Time) TimeT() linux.TimeT {
	return linux.NsecToTimeT(t.Nanoseconds())
}

// Timeval converts Time to a Linux timeval.
func (t Time) Timeval() linux.Timeval {
	return linux.NsecToTimeval(t.Nanoseconds())
}

// StatxTimestamp converts Time to a Linux statx_timestamp.
func (t Time) StatxTimestamp() linux.StatxTimestamp {
	return linux.NsecToStatxTimestamp(t.Nanoseconds())
}

// Add adds the duration of d to t.
func (t Time) Add(d time.Duration) Time {
	if t.ns > 0 && d.Nanoseconds() > math.MaxInt64-int64(t.ns) {
		return MaxTime
	}
	if t.ns < 0 && d.Nanoseconds() < math.MinInt64-int64(t.ns) {
		return MinTime
	}
	return Time{int64(t.ns) + d.Nanoseconds()}
}

// AddTime adds the duration of u to t.
func (t Time) AddTime(u Time) Time {
	return t.Add(time.Duration(u.ns))
}

// Equal reports whether the two times represent the same instant in time.
func (t Time) Equal(u Time) bool {
	return t.ns == u.ns
}

// Before reports whether the instant t is before the instant u.
func (t Time) Before(u Time) bool {
	return t.ns < u.ns
}

// After reports whether the instant t is after the instant u.
func (t Time) After(u Time) bool {
	return t.ns > u.ns
}

// Sub returns the duration of t - u.
//
// N.B. This measure may not make sense for every Time returned by ktime.Clock.
// Callers who need wall time duration can use ktime.Clock.WallTimeUntil to
// estimate that wall time.
func (t Time) Sub(u Time) time.Duration {
	dur := time.Duration(int64(t.ns)-int64(u.ns)) * time.Nanosecond
	switch {
	case u.Add(dur).Equal(t):
		return dur
	case t.Before(u):
		return MinDuration
	default:
		return MaxDuration
	}
}

// IsMin returns whether t represents the lowest possible time instant.
func (t Time) IsMin() bool {
	return t == MinTime
}

// IsZero returns whether t represents the zero time instant in t's Clock domain.
func (t Time) IsZero() bool {
	return t == ZeroTime
}

// String returns the time represented in nanoseconds as a string.
func (t Time) String() string {
	return fmt.Sprintf("%dns", t.Nanoseconds())
}

// A Clock is an abstract time source.
type Clock interface {
	// Now returns the current time in nanoseconds according to the Clock.
	Now() Time

	// WallTimeUntil returns the estimated wall time until Now will return a
	// value greater than or equal to t, given that a recent call to Now
	// returned now. If t has already passed, WallTimeUntil may return 0 or a
	// negative value.
	//
	// WallTimeUntil must be abstract to support Clocks that do not represent
	// wall time (e.g. thread group execution timers). Clocks that represent
	// wall times may embed the WallRateClock type to obtain an appropriate
	// trivial implementation of WallTimeUntil.
	//
	// WallTimeUntil is used to determine when associated Timers should next
	// check for expirations. Returning too small a value may result in
	// spurious Timer goroutine wakeups, while returning too large a value may
	// result in late expirations. Implementations should usually err on the
	// side of underestimating.
	WallTimeUntil(t, now Time) time.Duration

	// Waitable methods may be used to subscribe to Clock events. Waiters will
	// not be preserved by Save and must be re-established during restore.
	//
	// Since Clock events are transient, implementations of
	// waiter.Waitable.Readiness should return 0.
	waiter.Waitable
}

// WallRateClock implements Clock.WallTimeUntil for Clocks that elapse at the
// same rate as wall time.
type WallRateClock struct{}

// WallTimeUntil implements Clock.WallTimeUntil.
func (*WallRateClock) WallTimeUntil(t, now Time) time.Duration {
	return t.Sub(now)
}

// NoClockEvents implements waiter.Waitable for Clocks that do not generate
// events.
type NoClockEvents struct{}

// Readiness implements waiter.Waitable.Readiness.
func (*NoClockEvents) Readiness(mask waiter.EventMask) waiter.EventMask {
	return 0
}

// EventRegister implements waiter.Waitable.EventRegister.
func (*NoClockEvents) EventRegister(e *waiter.Entry) error {
	return nil
}

// EventUnregister implements waiter.Waitable.EventUnregister.
func (*NoClockEvents) EventUnregister(e *waiter.Entry) {
}

// ClockEventsQueue implements waiter.Waitable by wrapping waiter.Queue and
// defining waiter.Waitable.Readiness as required by Clock.
type ClockEventsQueue struct {
	waiter.Queue
}

// EventRegister implements waiter.Waitable.
func (c *ClockEventsQueue) EventRegister(e *waiter.Entry) error {
	c.Queue.EventRegister(e)
	return nil
}

// Readiness implements waiter.Waitable.Readiness.
func (*ClockEventsQueue) Readiness(mask waiter.EventMask) waiter.EventMask {
	return 0
}

// Listener receives expirations from a Timer.
type Listener interface {
	// NotifyTimer is called when its associated Timer expires. exp is the number
	// of expirations. setting is the next timer Setting.
	//
	// Notify is called with the associated Timer's mutex locked, so Notify
	// must not take any locks that precede Timer.mu in lock order.
	//
	// If Notify returns true, the timer will use the returned setting
	// rather than the passed one.
	//
	// Preconditions: exp > 0.
	NotifyTimer(exp uint64, setting Setting) (newSetting Setting, update bool)
}

// Setting contains user-controlled mutable Timer properties.
//
// +stateify savable
type Setting struct {
	// Enabled is true if the timer is running.
	Enabled bool

	// Next is the time in nanoseconds of the next expiration.
	Next Time

	// Period is the time in nanoseconds between expirations. If Period is
	// zero, the timer will not automatically restart after expiring.
	//
	// Invariant: Period >= 0.
	Period time.Duration
}

// SettingFromSpec converts a (value, interval) pair to a Setting based on a
// reading from c. value is interpreted as a time relative to c.Now().
func SettingFromSpec(value time.Duration, interval time.Duration, c Clock) (Setting, error) {
	return SettingFromSpecAt(value, interval, c.Now())
}

// SettingFromSpecAt converts a (value, interval) pair to a Setting. value is
// interpreted as a time relative to now.
func SettingFromSpecAt(value time.Duration, interval time.Duration, now Time) (Setting, error) {
	if value < 0 {
		return Setting{}, linuxerr.EINVAL
	}
	if value == 0 {
		return Setting{Period: interval}, nil
	}
	return Setting{
		Enabled: true,
		Next:    now.Add(value),
		Period:  interval,
	}, nil
}

// SettingFromAbsSpec converts a (value, interval) pair to a Setting. value is
// interpreted as an absolute time.
func SettingFromAbsSpec(value Time, interval time.Duration) (Setting, error) {
	if value.Before(ZeroTime) {
		return Setting{}, linuxerr.EINVAL
	}
	if value.IsZero() {
		return Setting{Period: interval}, nil
	}
	return Setting{
		Enabled: true,
		Next:    value,
		Period:  interval,
	}, nil
}

// SettingFromItimerspec converts a linux.Itimerspec to a Setting. If abs is
// true, its.Value is interpreted as an absolute time. Otherwise, it is
// interpreted as a time relative to c.Now().
func SettingFromItimerspec(its linux.Itimerspec, abs bool, c Clock) (Setting, error) {
	if abs {
		return SettingFromAbsSpec(FromTimespec(its.Value), its.Interval.ToDuration())
	}
	return SettingFromSpec(its.Value.ToDuration(), its.Interval.ToDuration(), c)
}

// SpecFromSetting converts a timestamp and a Setting to a (relative value,
// interval) pair, as used by most Linux syscalls that return a struct
// itimerval or struct itimerspec.
func SpecFromSetting(now Time, s Setting) (value, period time.Duration) {
	if !s.Enabled {
		return 0, s.Period
	}
	return s.Next.Sub(now), s.Period
}

// ItimerspecFromSetting converts a Setting to a linux.Itimerspec.
func ItimerspecFromSetting(now Time, s Setting) linux.Itimerspec {
	val, iv := SpecFromSetting(now, s)
	return linux.Itimerspec{
		Interval: linux.DurationToTimespec(iv),
		Value:    linux.DurationToTimespec(val),
	}
}

// At returns an updated Setting and a number of expirations after the
// associated Clock indicates a time of now.
//
// Settings may be created by successive calls to At with decreasing
// values of now (i.e. time may appear to go backward). Supporting this is
// required to support non-monotonic clocks, as well as allowing
// Timer.clock.Now() to be called without holding Timer.mu.
func (s Setting) At(now Time) (Setting, uint64) {
	if !s.Enabled {
		return s, 0
	}
	if s.Next.After(now) {
		return s, 0
	}
	if s.Period == 0 {
		s.Enabled = false
		return s, 1
	}
	exp := 1 + uint64(now.Sub(s.Next).Nanoseconds())/uint64(s.Period)
	s.Next = s.Next.Add(time.Duration(uint64(s.Period) * exp))
	return s, exp
}

// Timer is an optionally-periodic timer driven by sampling a user-specified
// Clock. Timer's semantics support the requirements of Linux's interval timers
// (setitimer(2), timer_create(2), timerfd_create(2)).
//
// Timers should be created using NewTimer and must be cleaned up by calling
// Timer.Destroy when no longer used.
//
// +stateify savable
type Timer struct {
	// clock is the time source. clock is protected by mu and clockSeq.
	clockSeq sync.SeqCount `state:"nosave"`
	clock    Clock

	// listener is notified of expirations. listener is immutable.
	listener Listener

	// mu protects the following mutable fields.
	mu sync.Mutex `state:"nosave"`

	// setting is the timer setting. setting is protected by mu.
	setting Setting

	pauseState timerPauseState

	// kicker is used to wake the Timer goroutine. The kicker pointer is
	// immutable, but its state is protected by mu.
	kicker *time.Timer `state:"nosave"`

	// entry is registered with clock.EventRegister. entry is immutable.
	//
	// Per comment in Clock, entry must be re-registered after restore; per
	// comment in Timer.Load, this is done in Timer.Resume.
	entry waiter.Entry `state:"nosave"`

	// events is the channel that will be notified whenever entry receives an
	// event. It is also closed by Timer.Destroy to instruct the Timer
	// goroutine to exit.
	events chan struct{} `state:"nosave"`
}

type timerPauseState uint8

const (
	// timerUnpaused indicates that the Timer is neither paused nor
	// destroyed.
	timerUnpaused timerPauseState = iota

	// timerPaused indicates that the Timer is paused, not destroyed.
	timerPaused

	// timerDestroyed indicates that the Timer is destroyed.
	timerDestroyed
)

// timerTickEvents are Clock events that require the Timer goroutine to Tick
// prematurely.
const timerTickEvents = ClockEventSet | ClockEventRateIncrease

// NewTimer returns a new Timer that will obtain time from clock and send
// expirations to listener. The Timer is initially stopped and has no first
// expiration or period configured.
func NewTimer(clock Clock, listener Listener) *Timer {
	t := &Timer{
		clock:    clock,
		listener: listener,
	}
	t.init()
	return t
}

// init initializes Timer state that is not preserved across save/restore. If
// init has already been called, calling it again is a no-op.
//
// Preconditions: t.mu must be locked, or the caller must have exclusive access
// to t.
func (t *Timer) init() {
	if t.kicker != nil {
		return
	}
	// If t.kicker is nil, the Timer goroutine can't be running, so we can't
	// race with it.
	t.kicker = time.NewTimer(0)
	t.entry, t.events = waiter.NewChannelEntry(timerTickEvents)
	if err := t.clock.EventRegister(&t.entry); err != nil {
		panic(err)
	}
	go t.runGoroutine() // S/R-SAFE: synchronized by t.mu
}

// Destroy releases resources owned by the Timer. Pause and Resume may be
// called on a Destroyed Timer and are no-ops. No other methods may be called
// on a Destroyed Timer.
func (t *Timer) Destroy() {
	// Stop the Timer, ensuring that the Timer goroutine will not call
	// t.kicker.Reset, before calling t.kicker.Stop.
	t.mu.Lock()
	t.setting.Enabled = false
	// Set timerDestroyed to prevent t.Tick() from mutating Timer state.
	t.pauseState = timerDestroyed
	t.mu.Unlock()
	t.kicker.Stop()
	// Unregister t.entry, ensuring that the Clock will not send to t.events,
	// before closing t.events to instruct the Timer goroutine to exit.
	t.clock.EventUnregister(&t.entry)
	close(t.events)
}

func (t *Timer) runGoroutine() {
	for {
		select {
		case <-t.kicker.C:
		case _, ok := <-t.events:
			if !ok {
				// Channel closed by Destroy.
				return
			}
		}
		t.Tick()
	}
}

// Tick requests that the Timer immediately check for expirations and
// re-evaluate when it should next check for expirations.
func (t *Timer) Tick() {
	// Optimistically read t.Clock().Now() before locking t.mu, as t.clock is
	// unlikely to change.
	unlockedClock := t.Clock()
	now := unlockedClock.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pauseState != timerUnpaused {
		return
	}
	if t.clock != unlockedClock {
		now = t.clock.Now()
	}
	s, exp := t.setting.At(now)
	t.setting = s
	if exp > 0 {
		if newS, ok := t.listener.NotifyTimer(exp, t.setting); ok {
			t.setting = newS
		}
	}
	t.resetKickerLocked(now)
}

// Pause pauses the Timer, ensuring that it does not generate any further
// expirations until Resume is called. If the Timer is already paused, Pause
// has no effect.
func (t *Timer) Pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pauseState != timerUnpaused {
		return
	}
	t.pauseState = timerPaused
	// t.kicker may be nil if we were restored but never resumed.
	if t.kicker != nil {
		t.kicker.Stop()
	}
}

// Resume ends the effect of Pause. If the Timer is not paused, Resume has no
// effect.
func (t *Timer) Resume() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pauseState != timerPaused {
		return
	}
	t.pauseState = timerUnpaused

	// Lazily initialize the Timer. We can't call Timer.init until Timer.Resume
	// because save/restore will restore Timers before
	// kernel.Timekeeper.SetClocks() has been called, so if t.clock is backed
	// by a kernel.Timekeeper then the Timer goroutine will panic if it calls
	// t.clock.Now().
	t.init()

	// Kick the Timer goroutine in case it was already initialized, but the
	// Timer goroutine was sleeping.
	t.kicker.Reset(0)
}

// Get returns a snapshot of the Timer's current Setting and the time
// (according to the Timer's Clock) at which the snapshot was taken.
//
// Preconditions: The Timer must not be paused (since its Setting cannot
// be advanced to the current time while it is paused.)
func (t *Timer) Get() (Time, Setting) {
	// Optimistically read t.Clock().Now() before locking t.mu, as t.clock is
	// unlikely to change.
	unlockedClock := t.Clock()
	now := unlockedClock.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pauseState != timerUnpaused {
		panic(fmt.Sprintf("Timer.Get called on Timer %p in pause state %v", t, t.pauseState))
	}
	if t.clock != unlockedClock {
		now = t.clock.Now()
	}
	s, exp := t.setting.At(now)
	t.setting = s
	if exp > 0 {
		if newS, ok := t.listener.NotifyTimer(exp, t.setting); ok {
			t.setting = newS
		}
	}
	t.resetKickerLocked(now)
	return now, s
}

// Swap atomically changes the Timer's Setting and returns the Timer's previous
// Setting and the time (according to the Timer's Clock) at which the snapshot
// was taken. Setting s.Enabled to true starts the Timer, while setting
// s.Enabled to false stops it.
//
// Preconditions: The Timer must not be paused.
func (t *Timer) Swap(s Setting) (Time, Setting) {
	return t.SwapAnd(s, nil)
}

// SwapAnd atomically changes the Timer's Setting, calls f if it is not nil,
// and returns the Timer's previous Setting and the time (according to the
// Timer's Clock) at which the Setting was changed. Setting s.Enabled to true
// starts the timer, while setting s.Enabled to false stops it.
//
// Preconditions:
//   - The Timer must not be paused.
//   - f cannot call any Timer methods since it is called with the Timer mutex
//     locked.
func (t *Timer) SwapAnd(s Setting, f func()) (Time, Setting) {
	// Optimistically read t.Clock().Now() before locking t.mu, as t.clock is
	// unlikely to change.
	unlockedClock := t.Clock()
	now := unlockedClock.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pauseState != timerUnpaused {
		panic(fmt.Sprintf("Timer.SwapAnd called on Timer %p in pause state %v", t, t.pauseState))
	}
	if t.clock != unlockedClock {
		now = t.clock.Now()
	}
	oldS, oldExp := t.setting.At(now)
	if oldExp > 0 {
		t.listener.NotifyTimer(oldExp, oldS)
		// N.B. The returned Setting doesn't matter because we're about
		// to overwrite.
	}
	if f != nil {
		f()
	}
	newS, newExp := s.At(now)
	t.setting = newS
	if newExp > 0 {
		if newS, ok := t.listener.NotifyTimer(newExp, t.setting); ok {
			t.setting = newS
		}
	}
	t.resetKickerLocked(now)
	return now, oldS
}

// SetClock atomically changes a Timer's Clock and Setting.
func (t *Timer) SetClock(c Clock, s Setting) {
	var now Time
	if s.Enabled {
		now = c.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.setting = s
	if oldC := t.clock; oldC != c {
		oldC.EventUnregister(&t.entry)
		c.EventRegister(&t.entry)
		t.clockSeq.BeginWrite()
		t.clock = c
		t.clockSeq.EndWrite()
	}
	t.resetKickerLocked(now)
}

// Preconditions: t.mu must be locked.
func (t *Timer) resetKickerLocked(now Time) {
	if t.setting.Enabled {
		// Clock.WallTimeUntil may return a negative value. This is fine;
		// time.when treats negative Durations as 0.
		t.kicker.Reset(t.clock.WallTimeUntil(t.setting.Next, now))
	}
	// We don't call t.kicker.Stop if !t.setting.Enabled because in most cases
	// resetKickerLocked will be called from the Timer goroutine itself, in
	// which case t.kicker has already fired and t.kicker.Stop will be an
	// expensive no-op (time.Timer.Stop => time.stopTimer => runtime.stopTimer
	// => runtime.deltimer).
}

// Clock returns the Clock used by t.
func (t *Timer) Clock() Clock {
	return SeqAtomicLoadClock(&t.clockSeq, &t.clock)
}

// ChannelNotifier is a Listener that sends on a channel.
//
// ChannelNotifier cannot be saved or loaded.
type ChannelNotifier chan struct{}

// NewChannelNotifier creates a new channel notifier.
//
// If the notifier is used with a timer, Timer.Destroy will close the channel
// returned here.
func NewChannelNotifier() (Listener, <-chan struct{}) {
	tchan := make(chan struct{}, 1)
	return ChannelNotifier(tchan), tchan
}

// NotifyTimer implements Listener.NotifyTimer.
func (c ChannelNotifier) NotifyTimer(uint64, Setting) (Setting, bool) {
	select {
	case c <- struct{}{}:
	default:
	}

	return Setting{}, false
}
