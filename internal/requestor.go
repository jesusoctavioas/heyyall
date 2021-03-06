// Copyright (c) 2020 Richard Youngkin. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package internal

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/youngkin/heyyall/api"
)

// Requestor is the component that will schedule requests to endpoints. It
// expects to be run as a goroutine.
type Requestor struct {
	// Context is used to cancel the goroutine
	Ctx context.Context
	// ResponseC is used to send the results of a request to the response handler
	ResponseC chan Response
	// Client is the target of the test run
	Client http.Client
}

// ResponseChan returns a chan Response
func (r Requestor) ResponseChan() chan Response {
	return r.ResponseC
}

// ProcessRqst runs the requests configured by 'ep' at the requested rate for either
// 'numRqsts' times or 'runDur' duration
func (r Requestor) ProcessRqst(ep api.Endpoint, numRqsts int, runDur time.Duration, rqstRate int) {
	if len(ep.URL) == 0 || len(ep.Method) == 0 {
		log.Warn().Msgf("Requestor - request contains an invalid endpoint %+v, URL or Method is empty", ep)
		return
	}

	// TODO: Add context to request
	req, err := http.NewRequest(ep.Method, ep.URL, bytes.NewBuffer([]byte(ep.RqstBody)))
	if err != nil {
		log.Warn().Err(err).Msgf("Requestor unable to create http request")
		return
	}

	// At this point we know one of numRqsts or runDur is non-zero. Whichever one
	// is non-zero will be set to a super-high number to effectively disable its
	// test in the for-loop below
	if numRqsts == 0 {
		log.Debug().Msgf("ProcessRqst: EP: %s, numRqsts %d was 0", ep.URL, numRqsts)
		// TODO: Need to come back here and ensure that each EP goroutine can only
		// run it's share of the api.MaxRqsts limit.
		numRqsts = api.MaxRqsts
	}
	if runDur == time.Duration(0) {
		log.Debug().Msgf("ProcessRqst: EP: %s, runDur %d was 0", ep.URL, runDur/time.Second)
		runDur = api.MaxRunDuration
	}

	log.Debug().Msgf("Setting 'timesUp' duration to %d seconds", runDur/time.Second)
	timesUp := time.After(runDur)
	for i := 0; i < numRqsts; i++ {
		start := time.Now()
		resp, err := r.Client.Do(req)
		if resp != nil {
			defer resp.Body.Close()
		}
		if err != nil {
			log.Warn().Err(err).Msgf("Requestor: error sending request")
			return // TODO: Should return here? This assumes that the error is persistent
		}
		select {
		case <-r.Ctx.Done():
			log.Debug().Msg("Requestor cancelled, exiting")
			return
		case <-timesUp:
			log.Debug().Msg("Requestor runDur expired, exiting")
			return
		case r.ResponseC <- Response{
			HTTPStatus:      resp.StatusCode,
			Endpoint:        api.Endpoint{URL: ep.URL, Method: ep.Method},
			RequestDuration: time.Since(start),
		}:
		}

		// Zero request rate is completely unthrottled
		if rqstRate == 0 {
			continue
		}
		since := time.Since(start)
		delta := (time.Second / time.Duration(rqstRate)) - since
		if delta < 0 {
			continue
		}
		time.Sleep(delta)

	}
}
