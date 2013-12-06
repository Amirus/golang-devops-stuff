// Copyright 2013 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"github.com/globocom/tsuru/app/bind"
	"github.com/globocom/tsuru/provision"
	"launchpad.net/gocheck"
	"sort"
)

func (s *S) TestUnitGetName(c *gocheck.C) {
	u := Unit{Name: "abcdef", app: &App{Name: "2112"}}
	c.Assert(u.GetName(), gocheck.Equals, "abcdef")
}

func (s *S) TestUnitGetMachine(c *gocheck.C) {
	u := Unit{Machine: 10}
	c.Assert(u.GetMachine(), gocheck.Equals, u.Machine)
}

func (s *S) TestUnitGetStatus(c *gocheck.C) {
	var tests = []struct {
		input    string
		expected provision.Status
	}{
		{"started", provision.StatusStarted},
		{"building", provision.StatusBuilding},
		{"down", provision.StatusDown},
	}
	for _, test := range tests {
		u := Unit{State: test.input}
		got := u.GetStatus()
		c.Assert(got, gocheck.Equals, test.expected)
	}
}

func (s *S) TestUnitAvailable(c *gocheck.C) {
	var tests = []struct {
		input    provision.Status
		expected bool
	}{
		{provision.StatusStarted, true},
		{provision.StatusUnreachable, true},
		{provision.StatusBuilding, false},
		{provision.StatusDown, false},
		{provision.StatusError, false},
	}
	for _, test := range tests {
		u := Unit{State: test.input.String()}
		c.Check(u.Available(), gocheck.Equals, test.expected)
	}
}

func (s *S) TestUnitShouldBeABinderUnit(c *gocheck.C) {
	var _ bind.Unit = &Unit{}
}

func (s *S) TestUnitSliceLen(c *gocheck.C) {
	units := UnitSlice{Unit{}, Unit{}}
	c.Assert(units.Len(), gocheck.Equals, 2)
}

func (s *S) TestUnitSliceLess(c *gocheck.C) {
	units := UnitSlice{
		Unit{Name: "b", State: provision.StatusDown.String()},
		Unit{Name: "d", State: provision.StatusBuilding.String()},
		Unit{Name: "e", State: provision.StatusStarted.String()},
		Unit{Name: "s", State: provision.StatusUnreachable.String()},
	}
	c.Assert(units.Less(0, 1), gocheck.Equals, true)
	c.Assert(units.Less(1, 2), gocheck.Equals, true)
	c.Assert(units.Less(2, 0), gocheck.Equals, false)
	c.Assert(units.Less(3, 2), gocheck.Equals, true)
	c.Assert(units.Less(3, 1), gocheck.Equals, false)
}

func (s *S) TestUnitSliceSwap(c *gocheck.C) {
	units := UnitSlice{
		Unit{Name: "b", State: provision.StatusDown.String()},
		Unit{Name: "f", State: provision.StatusBuilding.String()},
		Unit{Name: "g", State: provision.StatusStarted.String()},
	}
	units.Swap(0, 1)
	c.Assert(units[0].State, gocheck.Equals, provision.StatusBuilding.String())
	c.Assert(units[1].State, gocheck.Equals, provision.StatusDown.String())
}

func (s *S) TestUnitSliceSort(c *gocheck.C) {
	units := UnitSlice{
		Unit{Name: "f", State: provision.StatusBuilding.String()},
		Unit{Name: "g", State: provision.StatusStarted.String()},
		Unit{Name: "b", State: provision.StatusDown.String()},
	}
	c.Assert(sort.IsSorted(units), gocheck.Equals, false)
	sort.Sort(units)
	c.Assert(sort.IsSorted(units), gocheck.Equals, true)
}

func (s *S) TestGenerateUnitQuotaItems(c *gocheck.C) {
	var tests = []struct {
		app  *App
		want []string
		n    int
	}{
		{&App{Name: "black"}, []string{"black-0"}, 1},
		{&App{Name: "black", Units: []Unit{{QuotaItem: "black-1"}, {QuotaItem: "black-5"}}}, []string{"black-6"}, 1},
		{&App{Name: "white", Units: []Unit{{QuotaItem: "white-9"}}}, []string{"white-10"}, 1},
		{&App{}, []string{"-0"}, 1},
		{&App{Name: "white", Units: []Unit{{Name: "white/0"}}}, []string{"white-0"}, 1},
		{&App{Name: "white", Units: []Unit{{QuotaItem: "white-w"}}}, []string{"white-0"}, 1},
		{&App{Name: "white", Units: []Unit{{QuotaItem: "white-4"}}}, []string{"white-5", "white-6", "white-7"}, 3},
		{&App{Name: "black"}, []string{"black-0", "black-1", "black-2", "black-3"}, 4},
		{&App{Name: "white", Units: []Unit{{QuotaItem: "white-w"}}}, []string{"white-0", "white-1", "white-2"}, 3},
		{&App{Name: "black-white"}, []string{"black-white-0", "black-white-1", "black-white-2"}, 3},
	}
	for _, t := range tests {
		got := generateUnitQuotaItems(t.app, t.n)
		c.Check(got, gocheck.DeepEquals, t.want)
	}
}
