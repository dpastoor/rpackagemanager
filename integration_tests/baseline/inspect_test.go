package baseline

import (
	"context"
	"github.com/metrumresearchgroup/command"
	"github.com/sebdah/goldie/v2"
	"testing"
)

func TestInspectDeps(t *testing.T) {
	testCmd := command.New()
	ctx := context.TODO()
	res, err := testCmd.Run(ctx, "pkgr", "inspect", "--deps", "--json")
	if err != nil {
		panic(err)
	}
	g := goldie.New(t)
	g.Assert(t, "inspect-deps", []byte(res.Output))
}

func TestInspectReverseDeps(t *testing.T) {
	testCmd := command.New()
	ctx := context.TODO()
	res, err := testCmd.Run(ctx, "pkgr", "inspect", "--deps", "--reverse", "--json")
	if err != nil {
		panic(err)
	}
	g := goldie.New(t)
	g.Assert(t, "inspect-reverse-deps", []byte(res.Output))
}

