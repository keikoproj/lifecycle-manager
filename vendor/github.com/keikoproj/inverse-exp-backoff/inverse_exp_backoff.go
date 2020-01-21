package iebackoff

import (
	"errors"
	"time"
)

// IEBackoff is the basic struct used for Inverse Exponential Backoff.
type IEBackoff struct {
	// max and min are the starting and minimum durations for the backoff.
	max, min time.Duration
	// factor is the multiplication factor
	factor float64
	// retries is the max number of attempts to make before giving up.
	retries uint32
	// nextDelay is the delay that would be used in the next iteration.
	nextDelay float64
}

// NewIEBackoff creates and returns a new IEBackoff object
func NewIEBackoff(max time.Duration, min time.Duration, factor float64, maxRetries uint32) (*IEBackoff, error) {
	if factor >= 1.0 || factor <= 0.0 {
		return nil, errors.New("Factor should be between 0 and 1")
	}

	ieb := IEBackoff{
		max:       max,
		min:       min,
		factor:    factor,
		retries:   maxRetries,
		nextDelay: float64(max.Nanoseconds()),
	}
	return &ieb, nil
}

// Next is the main method for the inverse exponential backoff. It takes a function pointer
// and the arguments required for that function as parameters. The function passed as argument
// is expected to return a golang error object.
func (ieb *IEBackoff) Next() error {
	// Confirm there are retries left.
	if ieb.retries == 0 {
		return errors.New("No more retries left")
	}

	// Actually sleep for the given delay.
	time.Sleep(time.Duration(ieb.nextDelay))

	// Decrement the retries left.
	ieb.retries--

	// Calculate the delay for the next iteration.
	minNano := float64(ieb.min.Nanoseconds())
	newBackoffTime := ieb.nextDelay * ieb.factor
	if newBackoffTime > minNano {
		ieb.nextDelay = newBackoffTime
	} else {
		ieb.nextDelay = minNano
	}

	return nil
}
