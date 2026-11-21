package controller

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
)

const maxStartRetryWait = 5 * time.Minute

// retryWait is the delay before the next start attempt for this node.
// It follows UpdatePeriodic and never waits longer than five minutes.
// A non-positive period uses the same 60s default as getDefaultControllerConfig.
func retryWait(updatePeriodic int) time.Duration {
	sec := updatePeriodic
	if sec <= 0 {
		sec = 60
	}
	if sec > int(maxStartRetryWait/time.Second) {
		sec = int(maxStartRetryWait / time.Second)
	}
	return time.Duration(sec) * time.Second
}

func (c *Controller) retryUntilStarted() {
	wait := retryWait(c.config.UpdatePeriodic)
	for {
		timer := time.NewTimer(wait)
		select {
		case <-c.closeCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if c.closeCtx.Err() != nil {
			return
		}
		err := c.startOnce()
		if err == nil || c.closeCtx.Err() != nil {
			return
		}
		c.logStartFailure(err)
	}
}

func (c *Controller) logStartFailure(err error) {
	wait := retryWait(c.config.UpdatePeriodic)
	log.Printf("[%s: %d] start failed, retry in %s (http status %s): %s",
		c.clientInfo.NodeType, c.clientInfo.NodeID, wait, httpStatusText(err), startFailureSummary(err))
}

func (c *Controller) logFetchFailure(what string, err error) {
	log.Printf("[%s: %d] %s failed (http status %s): %s",
		c.clientInfo.NodeType, c.clientInfo.NodeID, what, httpStatusText(err), startFailureSummary(err))
}

func httpStatusText(err error) string {
	if code, ok := api.StatusOf(err); ok {
		return strconv.Itoa(code)
	}
	return "n/a"
}

// startFailureSummary keeps the status line and drops the response body.
// Panel bodies can contain REALITY private keys. The API key is not included.
func startFailureSummary(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, " body="); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.TrimSpace(msg)
	if len(msg) > 180 {
		msg = msg[:180]
	}
	return msg
}
