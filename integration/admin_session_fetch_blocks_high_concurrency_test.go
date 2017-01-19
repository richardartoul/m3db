// +build integration_disabled

// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHighConcAdminSessionFetchBlocksFromPeers(t *testing.T) {
	if testing.Short() {
		t.SkipNow() // Just skip if we're doing a short run
	}
	// Test setup
	concurrency := 64
	testOpts := newTestOptions().
		SetFetchSeriesBlocksBatchSize(8).
		SetFetchSeriesBlocksBatchConcurrency(concurrency)
	testSetup, err := newTestSetup(testOpts)
	require.NoError(t, err)

	defer testSetup.close()

	testSetup.storageOpts =
		testSetup.storageOpts.SetRetentionOptions(testSetup.storageOpts.RetentionOptions().
			SetBufferDrain(time.Second).
			SetRetentionPeriod(6 * time.Hour))
	blockSize := testSetup.storageOpts.RetentionOptions().BlockSize()

	// Start the server
	log := testSetup.storageOpts.InstrumentOptions().Logger()
	require.NoError(t, testSetup.startServer())

	// Stop the server
	defer func() {
		require.NoError(t, testSetup.stopServer())
		log.Debug("server is now down")
	}()

	// Write test data
	now := testSetup.getNowFn()
	seriesMaps := make(seriesMap)
	inputData := []struct {
		metricNames []string
		numPoints   int
		start       time.Time
	}{
		{[]string{"foo", "bar"}, 100, now},
		{[]string{"foo", "baz"}, 50, now.Add(blockSize)},
	}
	for _, input := range inputData {
		testSetup.setNowFn(input.start)
		testData := generateTestData(input.metricNames, input.numPoints, input.start)
		seriesMaps[input.start] = testData
		require.NoError(t, testSetup.writeBatch(testNamespaces[0], testData))
	}
	log.Debug("test data is now written")

	// Advance time and sleep for a long enough time so data blocks are sealed during ticking
	testSetup.setNowFn(testSetup.getNowFn().Add(blockSize * 2))
	later := testSetup.getNowFn()
	time.Sleep(testSetup.storageOpts.RetentionOptions().BufferDrain() * 4)

	results := make([]seriesMap, 0, concurrency)
	var wg sync.WaitGroup
	var mutex sync.Mutex
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			observedSeriesMaps := testSetupToSeriesMaps(t, testSetup, testNamespaces[0], now, later)
			mutex.Lock()
			results = append(results, observedSeriesMaps)
			mutex.Unlock()
			wg.Done()
		}()
	}
	wg.Wait()

	require.Equal(t, concurrency, len(results))
	// Verify retrieved data matches what we've written
	for _, obsSeriesMaps := range results {
		verifySeriesMapsEqual(t, seriesMaps, obsSeriesMaps)
	}
}