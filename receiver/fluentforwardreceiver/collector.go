// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fluentforwardreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/fluentforwardreceiver"

import (
	"context"

	"go.opencensus.io/stats"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/fluentforwardreceiver/observ"
)

// Collector acts as an aggregator of LogRecords so that we don't have to
// generate as many plog.Logs instances...we can pre-batch the LogRecord
// instances from several Forward events into one to hopefully reduce
// allocations and GC overhead.
type Collector struct {
	nextConsumer consumer.Logs
	eventCh      <-chan Event
	logger       *zap.Logger
	obsrecv      *obsreport.Receiver
}

func newCollector(eventCh <-chan Event, next consumer.Logs, logger *zap.Logger, obsrecv *obsreport.Receiver) *Collector {
	return &Collector{
		nextConsumer: next,
		eventCh:      eventCh,
		logger:       logger,
		obsrecv:      obsrecv,
	}
}

func (c *Collector) Start(ctx context.Context) {
	go c.processEvents(ctx)
}

func (c *Collector) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-c.eventCh:
			out := plog.NewLogs()
			rls := out.ResourceLogs().AppendEmpty()
			logSlice := rls.ScopeLogs().AppendEmpty().LogRecords()
			e.LogRecords().MoveAndAppendTo(logSlice)

			// Pull out anything waiting on the eventCh to get better
			// efficiency on LogResource allocations.
			c.fillBufferUntilChanEmpty(logSlice)

			stats.Record(context.Background(), observ.RecordsGenerated.M(int64(out.LogRecordCount())))
			ctx = c.obsrecv.StartLogsOp(ctx)
			err := c.nextConsumer.ConsumeLogs(ctx, out)
			c.obsrecv.EndLogsOp(ctx, "fluent", out.LogRecordCount(), err)
		}
	}
}

func (c *Collector) fillBufferUntilChanEmpty(dest plog.LogRecordSlice) {
	for {
		select {
		case e := <-c.eventCh:
			e.LogRecords().MoveAndAppendTo(dest)
		default:
			return
		}
	}
}
