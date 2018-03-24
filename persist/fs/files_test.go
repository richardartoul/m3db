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

package fs

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/m3db/m3db/digest"
	"github.com/m3db/m3db/retention"
	"github.com/m3db/m3db/storage/namespace"
	"github.com/m3db/m3x/ident"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testNs1ID       = ident.StringID("testNs")
	testNs2ID       = ident.StringID("testNs2")
	testNs1Metadata = func(t *testing.T) namespace.Metadata {
		md, err := namespace.NewMetadata(testNs1ID, namespace.NewOptions().SetRetentionOptions(
			retention.NewOptions().SetBlockSize(testBlockSize)))
		require.NoError(t, err)
		return md
	}
	testNs2Metadata = func(t *testing.T) namespace.Metadata {
		md, err := namespace.NewMetadata(testNs2ID, namespace.NewOptions().SetRetentionOptions(
			retention.NewOptions().SetBlockSize(testBlockSize)))
		require.NoError(t, err)
		return md
	}
)

func createTempFile(t *testing.T) *os.File {
	fd, err := ioutil.TempFile("", "testfile")
	require.NoError(t, err)
	return fd
}

func createTempDir(t *testing.T) string {
	dir, err := ioutil.TempDir("", "testdir")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func createDataFile(t *testing.T, shardDir string, blockStart time.Time, suffix string, b []byte) {
	filePath := filesetPathFromTime(shardDir, blockStart, suffix)
	createFile(t, filePath, b)
}

func createFile(t *testing.T, filePath string, b []byte) {
	fd, err := os.Create(filePath)
	require.NoError(t, err)
	if b != nil {
		fd.Write(b)
	}
	fd.Close()
}

func createInfoFilesSnapshotDir(t *testing.T, namespace ident.ID, shard uint32, iter int) string {
	return createInfoFiles(t, snapshotDirName, namespace, shard, iter)
}

func createInfoFilesDataDir(t *testing.T, namespace ident.ID, shard uint32, iter int) string {
	return createInfoFiles(t, dataDirName, namespace, shard, iter)
}

func createInfoFiles(t *testing.T, subDirName string, namespace ident.ID, shard uint32, iter int) string {
	dir := createTempDir(t)
	shardDir := path.Join(dir, subDirName, namespace.String(), strconv.Itoa(int(shard)))
	require.NoError(t, os.MkdirAll(shardDir, 0755))
	for i := 0; i < iter; i++ {
		ts := time.Unix(int64(i), 0)
		infoFilePath := filesetPathFromTime(shardDir, ts, infoFileSuffix)
		createFile(t, infoFilePath, nil)
	}
	return dir
}

func createCommitLogFiles(t *testing.T, iter, perSlot int) string {
	dir := createTempDir(t)
	commitLogsDir := path.Join(dir, commitLogsDirName)
	assert.NoError(t, os.Mkdir(commitLogsDir, 0755))
	for i := 0; i < iter; i++ {
		for j := 0; j < perSlot; j++ {
			filePath, _ := NextCommitLogsFile(dir, time.Unix(0, int64(i)))
			fd, err := os.Create(filePath)
			assert.NoError(t, err)
			assert.NoError(t, fd.Close())
		}
	}
	return dir
}

func validateCommitLogFiles(t *testing.T, slot, index, perSlot, resIdx int, dir string, files []string) {
	entry := fmt.Sprintf("%d%s%d", slot, separator, index)
	fileName := fmt.Sprintf("%s%s%s%s", commitLogFilePrefix, separator, entry, fileSuffix)

	x := (resIdx * perSlot) + index
	require.Equal(t, path.Join(dir, commitLogsDirName, fileName), files[x])
}

func TestOpenFilesFails(t *testing.T) {
	testFilePath := "/not/a/real/path"
	expectedErr := errors.New("synthetic error")

	opener := func(filePath string) (*os.File, error) {
		assert.Equal(t, filePath, testFilePath)
		return nil, expectedErr
	}

	var fd *os.File
	err := openFiles(opener, map[string]**os.File{
		testFilePath: &fd,
	})
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestCloseAllFails(t *testing.T) {
	file := createTempFile(t)
	defer os.Remove(file.Name())

	assert.NoError(t, file.Close())
	assert.Error(t, closeAll(file))
}

func TestDeleteFiles(t *testing.T) {
	var files []string
	iter := 3

	for i := 0; i < iter; i++ {
		fd := createTempFile(t)
		fd.Close()
		files = append(files, fd.Name())
	}

	// Add a non-existent file path
	files = append(files, "/not/a/real/path")

	require.Error(t, DeleteFiles(files))
	for i := 0; i < iter; i++ {
		require.True(t, !FileExists(files[i]))
	}
}

func TestDeleteInactiveDirectories(t *testing.T) {
	tempPrefix, err := ioutil.TempDir("", "filespath")
	require.NoError(t, err)
	defer func() {
		os.RemoveAll(tempPrefix)
	}()
	namespaceDir := NamespaceDataDirPath(tempPrefix, testNs1ID)

	// Test shard deletion within a namespace
	shards := []uint32{uint32(4), uint32(5), uint32(6)}
	shardDirs := []string{"4", "5", "6"}
	for _, shard := range shards {
		shardDir := ShardDataDirPath(tempPrefix, testNs1ID, shard)
		err := os.MkdirAll(shardDir, defaultNewDirectoryMode)
		require.NoError(t, err)

		shardPath := path.Join(shardDir, "data.txt")
		_, err = os.Create(shardPath)
		require.NoError(t, err)
	}

	activeShards := shardDirs[1:]
	err = DeleteInactiveDirectories(namespaceDir, activeShards)
	require.NoError(t, err)
	dirs, err := ioutil.ReadDir(namespaceDir)
	require.NoError(t, err)
	require.Equal(t, 2, len(dirs))
	os.RemoveAll(namespaceDir)
}

func TestByTimeAscending(t *testing.T) {
	files := []string{"foo/fileset-1-info.db", "foo/fileset-12-info.db", "foo/fileset-2-info.db"}
	expected := []string{"foo/fileset-1-info.db", "foo/fileset-2-info.db", "foo/fileset-12-info.db"}
	sort.Sort(byTimeAscending(files))
	require.Equal(t, expected, files)
}

func TestForEachInfoFile(t *testing.T) {
	dir := createTempDir(t)
	defer os.RemoveAll(dir)

	shard := uint32(0)
	shardDir := ShardDataDirPath(dir, testNs1ID, shard)
	require.NoError(t, os.MkdirAll(shardDir, os.ModeDir|os.FileMode(0755)))

	blockStart := time.Unix(0, 0)
	buf := digest.NewBuffer()

	// No checkpoint file
	createDataFile(t, shardDir, blockStart, infoFileSuffix, nil)

	// No digest file
	blockStart = blockStart.Add(time.Nanosecond)
	createDataFile(t, shardDir, blockStart, infoFileSuffix, nil)
	createDataFile(t, shardDir, blockStart, checkpointFileSuffix, buf)

	// Digest of digest mismatch
	blockStart = blockStart.Add(time.Nanosecond)
	digests := []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc}
	buf.WriteDigest(digest.Checksum(append(digests, 0xd)))
	createDataFile(t, shardDir, blockStart, infoFileSuffix, nil)
	createDataFile(t, shardDir, blockStart, digestFileSuffix, digests)
	createDataFile(t, shardDir, blockStart, checkpointFileSuffix, buf)

	// Info file digest mismatch
	blockStart = blockStart.Add(time.Nanosecond)

	buf.WriteDigest(digest.Checksum(digests))
	createDataFile(t, shardDir, blockStart, infoFileSuffix, []byte{0x1})
	createDataFile(t, shardDir, blockStart, digestFileSuffix, digests)
	createDataFile(t, shardDir, blockStart, checkpointFileSuffix, buf)

	// All digests match
	blockStart = blockStart.Add(time.Nanosecond)
	infoData := []byte{0x1, 0x2, 0x3, 0x4}

	buf.WriteDigest(digest.Checksum(infoData))

	digestOfDigest := append(buf, []byte{0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc}...)
	buf.WriteDigest(digest.Checksum(digestOfDigest))
	createDataFile(t, shardDir, blockStart, infoFileSuffix, infoData)
	createDataFile(t, shardDir, blockStart, digestFileSuffix, digestOfDigest)
	createDataFile(t, shardDir, blockStart, checkpointFileSuffix, buf)

	var fnames []string
	var res []byte
	forEachInfoFile(dir, testNs1ID, shard, testReaderBufferSize, func(fname string, data []byte) {
		fnames = append(fnames, fname)
		res = append(res, data...)
	})

	require.Equal(t, []string{filesetPathFromTime(shardDir, blockStart, infoFileSuffix)}, fnames)
	require.Equal(t, infoData, res)
}

func TestTimeFromName(t *testing.T) {
	_, err := TimeFromFileName("foo/bar")
	require.Error(t, err)
	require.Equal(t, "unexpected file name foo/bar", err.Error())

	_, err = TimeFromFileName("foo/bar-baz")
	require.Error(t, err)

	v, err := TimeFromFileName("foo-1-bar.db")
	expected := time.Unix(0, 1)
	require.Equal(t, expected, v)
	require.NoError(t, err)

	v, err = TimeFromFileName("foo/bar/foo-21234567890-bar.db")
	expected = time.Unix(0, 21234567890)
	require.Equal(t, expected, v)
	require.NoError(t, err)
}

func TestTimeAndIndexFromCommitlogFileName(t *testing.T) {
	_, _, err := TimeAndIndexFromFileNameCommitlog("foo/bar")
	require.Error(t, err)
	require.Equal(t, "unexpected file name foo/bar", err.Error())

	_, _, err = TimeAndIndexFromFileNameCommitlog("foo/bar-baz")
	require.Error(t, err)

	type expected struct {
		t time.Time
		i int
	}
	ts, i, err := TimeAndIndexFromFileNameCommitlog("foo-1-0.db")
	exp := expected{time.Unix(0, 1), 0}
	require.Equal(t, exp.t, ts)
	require.Equal(t, exp.i, i)
	require.NoError(t, err)

	ts, i, err = TimeAndIndexFromFileNameCommitlog("foo/bar/foo-21234567890-1.db")
	exp = expected{time.Unix(0, 21234567890), 1}
	require.Equal(t, exp.t, ts)
	require.Equal(t, exp.i, i)
	require.NoError(t, err)
}

func TestFileExists(t *testing.T) {
	dir := createTempDir(t)
	defer os.RemoveAll(dir)

	shard := uint32(10)
	start := time.Now()
	shardDir := ShardDataDirPath(dir, testNs1ID, shard)
	err := os.MkdirAll(shardDir, defaultNewDirectoryMode)
	require.NoError(t, err)

	infoFilePath := filesetPathFromTime(shardDir, start, infoFileSuffix)
	createDataFile(t, shardDir, start, infoFileSuffix, nil)
	require.True(t, FileExists(infoFilePath))
	require.False(t, DataFilesetExistsAt(dir, testNs1ID, uint32(shard), start))

	checkpointFilePath := filesetPathFromTime(shardDir, start, checkpointFileSuffix)
	createDataFile(t, shardDir, start, checkpointFileSuffix, nil)
	require.True(t, FileExists(checkpointFilePath))
	require.True(t, DataFilesetExistsAt(dir, testNs1ID, uint32(shard), start))

	os.Remove(infoFilePath)
	require.False(t, FileExists(infoFilePath))
}

func TestShardDirPath(t *testing.T) {
	require.Equal(t, "foo/bar/data/testNs/12", ShardDataDirPath("foo/bar", testNs1ID, 12))
	require.Equal(t, "foo/bar/data/testNs/12", ShardDataDirPath("foo/bar/", testNs1ID, 12))
}

func TestFilePathFromTime(t *testing.T) {
	start := time.Unix(1465501321, 123456789)
	inputs := []struct {
		prefix   string
		suffix   string
		expected string
	}{
		{"foo/bar", infoFileSuffix, "foo/bar/fileset-1465501321123456789-info.db"},
		{"foo/bar", indexFileSuffix, "foo/bar/fileset-1465501321123456789-index.db"},
		{"foo/bar", dataFileSuffix, "foo/bar/fileset-1465501321123456789-data.db"},
		{"foo/bar", checkpointFileSuffix, "foo/bar/fileset-1465501321123456789-checkpoint.db"},
		{"foo/bar/", infoFileSuffix, "foo/bar/fileset-1465501321123456789-info.db"},
	}
	for _, input := range inputs {
		require.Equal(t, input.expected, filesetPathFromTime(input.prefix, start, input.suffix))
	}
}

func TestFilesetFilesBefore(t *testing.T) {
	shard := uint32(0)
	dir := createInfoFilesDataDir(t, testNs1ID, shard, 20)
	defer os.RemoveAll(dir)

	cutoffIter := 8
	cutoff := time.Unix(int64(cutoffIter), 0)
	res, err := FilesetBefore(dir, testNs1ID, shard, cutoff)
	require.NoError(t, err)
	require.Equal(t, cutoffIter, len(res))

	shardDir := path.Join(dir, dataDirName, testNs1ID.String(), strconv.Itoa(int(shard)))
	for i := 0; i < len(res); i++ {
		ts := time.Unix(int64(i), 0)
		require.Equal(t, filesetPathFromTime(shardDir, ts, infoFileSuffix), res[i])
	}
}

func TestSnapshotFiles(t *testing.T) {
	shard := uint32(0)
	dir := createInfoFilesSnapshotDir(t, testNs1ID, shard, 20)
	defer os.RemoveAll(dir)

	files, err := SnapshotFiles(dir, testNs1ID, shard)
	require.NoError(t, err)
	require.Equal(t, 20, len(files))
	for i, snapshotFile := range files {
		require.Equal(t, int64(i), snapshotFile.BlockStart.Unix())
	}
}

func TestSnapshotDirPath(t *testing.T) {
	require.Equal(t, "prefix/snapshots", SnapshotDirPath("prefix"))
}

func TestNamespaceSnapshotsDirPath(t *testing.T) {
	expected := "prefix/snapshots/testNs"
	actual := NamespaceSnapshotsDirPath("prefix", testNs1ID)
	require.Equal(t, expected, actual)
}

func TestShardSnapshotsDirPath(t *testing.T) {
	expected := "prefix/snapshots/testNs/0"
	actual := ShardSnapshotsDirPath("prefix", testNs1ID, 0)
	require.Equal(t, expected, actual)
}

func TestSnapshotFilesetExistsAt(t *testing.T) {
	shard := uint32(0)
	ts := time.Unix(0, 0)
	dir := createTempDir(t)
	shardPath := ShardSnapshotsDirPath(dir, testNs1ID, 0)
	require.NoError(t, os.MkdirAll(shardPath, 0755))

	filePath := snapshotPathFromTimeAndIndex(shardPath, ts, checkpointFileSuffix, 0)
	createFile(t, filePath, []byte{})

	exists, err := SnapshotFilesetExistsAt(dir, testNs1ID, shard, ts)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestCommitLogFilesBefore(t *testing.T) {
	iter := 20
	perSlot := 3
	dir := createCommitLogFiles(t, iter, perSlot)
	defer os.RemoveAll(dir)

	cutoffIter := 8
	cutoff := time.Unix(0, int64(cutoffIter))
	commitLogsDir := CommitLogsDirPath(dir)
	files, err := CommitLogFilesBefore(commitLogsDir, cutoff)
	require.NoError(t, err)
	require.Equal(t, cutoffIter*perSlot, len(files))
	for i := 0; i < cutoffIter; i++ {
		for j := 0; j < perSlot; j++ {
			validateCommitLogFiles(t, i, j, perSlot, i, dir, files)
		}
	}
}

func TestCommitLogFilesForTime(t *testing.T) {
	iter := 20
	perSlot := 3
	dir := createCommitLogFiles(t, iter, perSlot)
	defer os.RemoveAll(dir)

	cutoffIter := 8
	cutoff := time.Unix(0, int64(cutoffIter))
	commitLogsDir := CommitLogsDirPath(dir)
	files, err := CommitLogFilesForTime(commitLogsDir, cutoff)
	require.NoError(t, err)

	for j := 0; j < perSlot; j++ {
		validateCommitLogFiles(t, cutoffIter, j, perSlot, 0, dir, files)
	}
}

func TestCommitLogFiles(t *testing.T) {
	iter := 20
	perSlot := 3
	dir := createCommitLogFiles(t, iter, perSlot)
	defer os.RemoveAll(dir)

	createFile(t, path.Join(dir, "abcd"), nil)
	createFile(t, path.Join(dir, strconv.Itoa(perSlot+1)+fileSuffix), nil)
	createFile(t, path.Join(dir, strconv.Itoa(iter+1)+separator+strconv.Itoa(perSlot+1)+fileSuffix), nil)
	createFile(t, path.Join(dir, separator+strconv.Itoa(iter+1)+separator+strconv.Itoa(perSlot+1)+fileSuffix), nil)

	files, err := CommitLogFiles(CommitLogsDirPath(dir))
	require.NoError(t, err)
	require.Equal(t, iter*perSlot, len(files))

	for i := 0; i < iter; i++ {
		for j := 0; j < perSlot; j++ {
			validateCommitLogFiles(t, i, j, perSlot, i, dir, files)
		}
	}
}
