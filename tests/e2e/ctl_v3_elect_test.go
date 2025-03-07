// Copyright 2016 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/pkg/v3/expect"
	"go.etcd.io/etcd/tests/v3/framework/e2e"
)

func TestCtlV3Elect(t *testing.T) {
	testCtl(t, testElect)
}

func testElect(cx ctlCtx) {
	name := "a"

	holder, ch, err := ctlV3Elect(cx, name, "p1", false)
	if err != nil {
		cx.t.Fatal(err)
	}

	l1 := ""
	select {
	case <-time.After(2 * time.Second):
		cx.t.Fatalf("timed out electing")
	case l1 = <-ch:
		if !strings.HasPrefix(l1, name) {
			cx.t.Errorf("got %q, expected %q prefix", l1, name)
		}
	}

	// blocked process that won't win the election
	blocked, ch, err := ctlV3Elect(cx, name, "p2", true)
	if err != nil {
		cx.t.Fatal(err)
	}
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ch:
		cx.t.Fatalf("should block")
	}

	// overlap with a blocker that will win the election
	blockAcquire, ch, err := ctlV3Elect(cx, name, "p2", false)
	if err != nil {
		cx.t.Fatal(err)
	}
	defer blockAcquire.Stop()
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ch:
		cx.t.Fatalf("should block")
	}

	// kill blocked process with clean shutdown
	if err = blocked.Signal(os.Interrupt); err != nil {
		cx.t.Fatal(err)
	}
	err = e2e.CloseWithTimeout(blocked, time.Second)
	if err != nil {
		// due to being blocked, this can potentially get killed and thus exit non-zero sometimes
		require.ErrorContains(cx.t, err, "unexpected exit code")
	}

	// kill the holder with clean shutdown
	if err = holder.Signal(os.Interrupt); err != nil {
		cx.t.Fatal(err)
	}
	if err = e2e.CloseWithTimeout(holder, time.Second); err != nil {
		cx.t.Fatal(err)
	}

	// blockAcquire should win the election
	select {
	case <-time.After(time.Second):
		cx.t.Fatalf("timed out from waiting to holding")
	case l2 := <-ch:
		if l1 == l2 || !strings.HasPrefix(l2, name) {
			cx.t.Fatalf("expected different elect name, got l1=%q, l2=%q", l1, l2)
		}
	}
}

// ctlV3Elect creates a elect process with a channel listening for when it wins the election.
func ctlV3Elect(cx ctlCtx, name, proposal string, expectFailure bool) (*expect.ExpectProcess, <-chan string, error) {
	cmdArgs := append(cx.PrefixArgs(), "elect", name, proposal)
	proc, err := e2e.SpawnCmd(cmdArgs, cx.envMap)
	outc := make(chan string, 1)
	if err != nil {
		close(outc)
		return proc, outc, err
	}
	go func() {
		s, xerr := proc.ExpectFunc(context.TODO(), func(string) bool { return true })
		if xerr != nil {
			if !expectFailure {
				cx.t.Errorf("expect failed (%v)", xerr)
			}
		}
		outc <- s
	}()
	return proc, outc, err
}
