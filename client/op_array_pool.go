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

package client

import (
	"github.com/m3db/m3db/interfaces/m3db"
	"github.com/m3db/m3db/pool"
)

type opArrayPool interface {
	// Get an array of ops
	Get() []m3db.Op

	// Put an array of ops
	Put(w []m3db.Op)
}

type poolOfOpArray struct {
	pool m3db.ObjectPool
}

func newOpArrayPool(size int, capacity int) opArrayPool {
	p := pool.NewObjectPool(size)
	p.Init(func() interface{} {
		return make([]m3db.Op, 0, capacity)
	})
	return &poolOfOpArray{p}
}

func (p *poolOfOpArray) Get() []m3db.Op {
	return p.pool.Get().([]m3db.Op)
}

func (p *poolOfOpArray) Put(ops []m3db.Op) {
	ops = ops[:0]
	p.pool.Put(ops)
}