package gocb

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type aggregatingMeterGroup struct {
	lock      sync.Mutex
	recorders map[string]*aggregatingValueRecorder
}

func (amg *aggregatingMeterGroup) Recorders() []*aggregatingValueRecorder {
	amg.lock.Lock()
	if len(amg.recorders) == 0 {
		amg.lock.Unlock()
		return []*aggregatingValueRecorder{}
	}
	recorders := make([]*aggregatingValueRecorder, len(amg.recorders))
	var i int
	for _, r := range amg.recorders {
		recorders[i] = r
		i++
	}
	amg.lock.Unlock()

	return recorders
}

// AggregatingMeter is a Meter implementation providing a simplified, but useful, view into current SDK state.
type AggregatingMeter struct {
	interval time.Duration

	valueRecorderGroups map[string]*aggregatingMeterGroup
	stopCh              chan struct{}
}

type AggregatingMeterOptions struct {
	EmitInterval time.Duration
}

func NewAggregatingMeter(opts *AggregatingMeterOptions) *AggregatingMeter {
	if opts == nil {
		opts = &AggregatingMeterOptions{}
	}
	interval := opts.EmitInterval
	if interval == 0 {
		interval = 10 * time.Minute
	}
	am := &AggregatingMeter{
		interval: interval,
		valueRecorderGroups: map[string]*aggregatingMeterGroup{
			meterValueServiceKV: {
				recorders: make(map[string]*aggregatingValueRecorder),
			},
			meterValueServiceViews: {
				recorders: make(map[string]*aggregatingValueRecorder),
			},
			meterValueServiceQuery: {
				recorders: make(map[string]*aggregatingValueRecorder),
			},
			meterValueServiceSearch: {
				recorders: make(map[string]*aggregatingValueRecorder),
			},
			meterValueServiceAnalytics: {
				recorders: make(map[string]*aggregatingValueRecorder),
			},
			meterValueServiceManagement: {
				recorders: make(map[string]*aggregatingValueRecorder),
			},
		},
		stopCh: make(chan struct{}),
	}

	return am
}

func (am *AggregatingMeter) startLoggerRoutine() {
	go am.loggerRoutine()
}

func (am *AggregatingMeter) loggerRoutine() {
	for {
		select {
		case <-am.stopCh:
			return
		case <-time.After(am.interval):
		}

		jsonData := am.generateOutput()
		if len(jsonData) == 1 {
			// Nothing to log so make sure we don't just log empty objects.
			return
		}

		jsonBytes, err := json.Marshal(jsonData)
		if err != nil {
			logDebugf("Failed to generate threshold logging service JSON: %s", err)
		}

		logInfof("Aggregate metrics: %s", jsonBytes)
	}
}

func (am *AggregatingMeter) generateOutput() map[string]interface{} {
	output := make(map[string]interface{})
	output["meta"] = map[string]interface{}{
		"emit_interval_s": am.interval,
	}

	for serviceName, group := range am.valueRecorderGroups {
		serviceMap := make(map[string]interface{})

		recorders := group.Recorders()
		if len(recorders) == 0 {
			continue
		}
		for _, recorder := range recorders {
			serviceMap[recorder.peerName] = recorder.GetAndResetValues()
		}
		if len(serviceMap) > 0 {
			output[serviceName] = serviceMap
		}
	}

	return output
}

func (am *AggregatingMeter) Counter(_ string, _ map[string]string) (Counter, error) {
	return defaultNoopCounter, nil
}

func (am *AggregatingMeter) ValueRecorder(name string, tags map[string]string) (ValueRecorder, error) {
	if name != meterNameResponses {
		return defaultNoopValueRecorder, nil
	}

	service, ok := tags[meterAttribServiceKey]
	if !ok {
		return defaultNoopValueRecorder, nil
	}

	if _, ok := am.valueRecorderGroups[service]; !ok {
		return defaultNoopValueRecorder, nil
	}

	peerName, ok := tags[meterAttribPeerName]
	if !ok {
		return defaultNoopValueRecorder, nil
	}
	// We don't need to lock around accessing recorder groups itself, it must never be modified.
	recorderGroup := am.valueRecorderGroups[service]
	recorderGroup.lock.Lock()
	recorder := recorderGroup.recorders[peerName]
	if recorder == nil {
		recorder = newAggregatingValueRecorder(peerName)
		recorderGroup.recorders[peerName] = recorder
	}
	recorderGroup.lock.Unlock()

	return recorder, nil
}

func (am *AggregatingMeter) close() {
	am.stopCh <- struct{}{}
}

type latencyHistogram struct {
	bins        []uint64
	maxValue    float64
	scaleFactor float64
	ratioLog    float64
	commonRatio float64
	startValue  float64
}

type cumulativeLatencyHistogram struct {
	bins        []uint64
	commonRatio float64
	startValue  float64
}

func newLatencyHistogram(maxValue, startValue float64, commonRatio float64) *latencyHistogram {
	ratio := math.Log(commonRatio)
	// We plus two so that values > maxValue and values <= startValue will have a bin to go into
	numBuckets := math.Ceil(math.Log(maxValue/startValue)/ratio) + 2

	return &latencyHistogram{
		bins:        make([]uint64, int(numBuckets)),
		maxValue:    maxValue,
		scaleFactor: startValue,
		ratioLog:    ratio,
		startValue:  startValue,
		commonRatio: commonRatio,
	}
}

func (lh *latencyHistogram) RecordValue(value uint64) {
	var bin int
	v := float64(value)
	if v > lh.maxValue {
		bin = len(lh.bins) - 1
	} else if v <= lh.scaleFactor {
		bin = 0
	} else {
		bin = int(math.Ceil(math.Log(v/lh.scaleFactor) / lh.ratioLog))
	}

	atomic.AddUint64(&lh.bins[bin], 1)
}

func (lh *latencyHistogram) AggregateAndReset() *cumulativeLatencyHistogram {
	bins := make([]uint64, len(lh.bins))
	var countSoFar uint64
	for i := 0; i < len(lh.bins); i++ {
		thisCount := atomic.SwapUint64(&lh.bins[i], 0)
		countSoFar += thisCount
		bins[i] = countSoFar
	}

	return &cumulativeLatencyHistogram{
		bins:        bins,
		commonRatio: lh.commonRatio,
		startValue:  lh.startValue,
	}
}

func (lhs *cumulativeLatencyHistogram) TotalCount() uint64 {
	return lhs.bins[len(lhs.bins)-1]
}

func (lhs *cumulativeLatencyHistogram) BinAtPercentile(percentile float64) string {
	c := lhs.TotalCount()
	count := uint64(math.Ceil((percentile / 100) * float64(c)))
	for i, bin := range lhs.bins {
		if bin >= count {
			if i == len(lhs.bins)-1 {
				return fmt.Sprintf("> %.2f", math.Pow(lhs.commonRatio, float64(i-1))*lhs.startValue)
			}
			return fmt.Sprintf("<= %.2f", math.Pow(lhs.commonRatio, float64(i))*lhs.startValue)
		}
	}

	return "0.0"
}

type aggregatingValueRecorder struct {
	peerName string
	hist     *latencyHistogram
}

func newAggregatingValueRecorder(peerName string) *aggregatingValueRecorder {
	return &aggregatingValueRecorder{
		peerName: peerName,
		hist:     newLatencyHistogram(2000000, 1000, 1.5),
	}
}

func (bc *aggregatingValueRecorder) RecordValue(val uint64) {
	bc.hist.RecordValue(val)
}

func (bc *aggregatingValueRecorder) GetAndResetValues() map[string]interface{} {
	hist := bc.hist.AggregateAndReset()
	return map[string]interface{}{
		"total_count": hist.TotalCount(),
		"percentiles_us": map[string]string{
			"50.0":  hist.BinAtPercentile(50.0),
			"90.0":  hist.BinAtPercentile(90.0),
			"99.0":  hist.BinAtPercentile(99.0),
			"99.9":  hist.BinAtPercentile(99.9),
			"100.0": hist.BinAtPercentile(100),
		},
	}
}
