package retry

import (
	"math/rand"
	"time"
)

func getDelay(o *retryOptions, tries int) time.Duration {
	// Exponential backoff with jitter to prevent thundering herd
	baseDelay := o.Delay * time.Duration(1<<(tries-1)) // 2^(tries-1) * Delay
	
	// Add jitter: ±25% randomization to prevent synchronized retries
	jitterRange := baseDelay / 2
	jitter := time.Duration(rand.Int63n(int64(jitterRange))) - time.Duration(baseDelay/4)
	delay := baseDelay + jitter
	
	if o.MaxDelay == 0 {
		return delay
	}

	if delay > o.MaxDelay {
		return o.MaxDelay
	}
	return delay
}

func Do[T any](options *retryOptions, callback func(try int) (T, error)) (T, error) {
	var tries int

	for {
		tries++

		res, err := callback(tries)
		if err == nil {
			return res, nil
		}

		switch err := err.(type) {
		case *ExitRetry:
			return res, err.Err
		case *ContinueRetry:
			if !canRetry(options, tries) {
				return res, err.Err
			}

			time.Sleep(getDelay(options, tries))
		default:
			return res, err
		}
	}
}
