// +build integration

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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/m3db/m3db/integration/generate"
	"github.com/m3db/m3db/retention"
	"github.com/m3db/m3db/storage/bootstrap"
	"github.com/m3db/m3db/storage/bootstrap/bootstrapper"
	"github.com/m3db/m3db/storage/bootstrap/bootstrapper/fs"
	"github.com/m3db/m3db/storage/bootstrap/result"
)

func TestFilesystemBootstrap(t *testing.T) {
	if testing.Short() {
		t.SkipNow() // Just skip if we're doing a short run
	}

	// Test setup
	tickInterval := 3 * time.Second
	opts := newTestOptions().
		SetTickInterval(tickInterval)

	setup, err := newTestSetup(opts)
	require.NoError(t, err)
	defer setup.close()

	var (
		ropts     = retention.NewOptions().SetRetentionPeriod(2 * time.Hour)
		blockSize = ropts.BlockSize()
	)
	setup.storageOpts = setup.storageOpts.SetTickInterval(tickInterval)
	require.NoError(t, setup.setRetentionOnAll(ropts))

	fsOpts := setup.storageOpts.CommitLogOptions().FilesystemOptions()
	filePathPrefix := fsOpts.FilePathPrefix()
	noOpAll := bootstrapper.NewNoOpAllBootstrapper()
	bsOpts := result.NewOptions()
	bfsOpts := fs.NewOptions().
		SetResultOptions(bsOpts).
		SetFilesystemOptions(fsOpts)
	bs, err := fs.NewFileSystemBootstrapper(filePathPrefix, bfsOpts, noOpAll)
	require.NoError(t, err)
	process := bootstrap.NewProcess(bs, bsOpts)

	setup.storageOpts = setup.storageOpts.
		SetBootstrapProcess(process)

	// Write test data
	now := setup.getNowFn()
	seriesMaps := generate.BlocksByStart([]generate.BlockConfig{
		{[]string{"foo", "bar"}, 100, now.Add(-blockSize)},
		{[]string{"foo", "baz"}, 50, now},
	})
	testNs, err := setup.storageOpts.NamespaceRegistry().Get(testNamespaces[0])
	require.NoError(t, err)
	require.NoError(t, writeTestDataToDisk(testNs, setup, seriesMaps))
	testNs, err = setup.storageOpts.NamespaceRegistry().Get(testNamespaces[1])
	require.NoError(t, err)
	require.NoError(t, writeTestDataToDisk(testNs, setup, nil))

	// Start the server with filesystem bootstrapper
	log := setup.storageOpts.InstrumentOptions().Logger()
	log.Debug("filesystem bootstrap test")
	require.NoError(t, setup.startServer())
	log.Debug("server is now up")

	// Stop the server
	defer func() {
		require.NoError(t, setup.stopServer())
		log.Debug("server is now down")
	}()

	// Verify in-memory data match what we expect
	verifySeriesMaps(t, setup, testNamespaces[0], seriesMaps)
	verifySeriesMaps(t, setup, testNamespaces[1], nil)
}
