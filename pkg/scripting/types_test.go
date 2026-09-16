package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("NIC", func() {
	It("exposes all fields", func() {
		nic := NewNIC(models.NIC{
			MAC: "00:50:56:a1:b2:c3", Network: "VM Network",
			IPv4Address: "10.0.0.5", IPv6Address: "fe80::1", Index: 0,
		})
		g := starlark.StringDict{"nic": nic}
		r := execScript(g, `
mac = nic.mac
network = nic.network
ipv4 = nic.ipv4
ipv6 = nic.ipv6
index = nic.index
`)
		Expect(strVal(r, "mac")).To(Equal("00:50:56:a1:b2:c3"))
		Expect(strVal(r, "network")).To(Equal("VM Network"))
		Expect(strVal(r, "ipv4")).To(Equal("10.0.0.5"))
		Expect(strVal(r, "ipv6")).To(Equal("fe80::1"))
		Expect(intVal(r, "index")).To(Equal(int64(0)))
	})
})

var _ = Describe("Issue", func() {
	It("exposes all fields", func() {
		issue := NewIssue(models.Issue{
			Label: "VMware Tools not installed", Description: "Guest OS lacks VMware Tools",
			Category: "Warning",
		})
		g := starlark.StringDict{"issue": issue}
		r := execScript(g, `
label = issue.label
description = issue.description
category = issue.category
`)
		Expect(strVal(r, "label")).To(Equal("VMware Tools not installed"))
		Expect(strVal(r, "description")).To(Equal("Guest OS lacks VMware Tools"))
		Expect(strVal(r, "category")).To(Equal("Warning"))
	})
})

var _ = Describe("Application", func() {
	It("exposes name and process", func() {
		app := NewApplication(models.GuestApp{Name: "httpd", Version: "/usr/sbin/httpd"})
		g := starlark.StringDict{"app": app}
		r := execScript(g, `
name = app.name
process = app.process
`)
		Expect(strVal(r, "name")).To(Equal("httpd"))
		Expect(strVal(r, "process")).To(Equal("/usr/sbin/httpd"))
	})
})

var _ = Describe("ApplicationList", func() {
	var apps *ApplicationList

	BeforeEach(func() {
		apps = NewApplicationList([]*Application{
			NewApplication(models.GuestApp{Name: "httpd", Version: "/usr/sbin/httpd"}),
			NewApplication(models.GuestApp{Name: "sshd", Version: "/usr/sbin/sshd"}),
			NewApplication(models.GuestApp{Name: "netscaler-agent", Version: "/opt/ns/bin/agent"}),
		})
	})

	It("supports len and indexing", func() {
		g := starlark.StringDict{"apps": apps}
		r := execScript(g, `
count = len(apps)
first = apps[0].name
`)
		Expect(intVal(r, "count")).To(Equal(int64(3)))
		Expect(strVal(r, "first")).To(Equal("httpd"))
	})

	It("supports iteration", func() {
		g := starlark.StringDict{"apps": apps}
		r := execScript(g, `count = len([a for a in apps])`)
		Expect(intVal(r, "count")).To(Equal(int64(3)))
	})

	Describe("filter", func() {
		It("filters by predicate", func() {
			g := starlark.StringDict{"apps": apps}
			r := execScript(g, `count = len(apps.filter(lambda a: "sshd" in a.name))`)
			Expect(intVal(r, "count")).To(Equal(int64(1)))
		})

		It("returns ApplicationList that supports chaining", func() {
			g := starlark.StringDict{"apps": apps}
			r := execScript(g, `
filtered = apps.filter(lambda a: "ssh" in a.name or "http" in a.name)
names = filtered.map(lambda a: a.name)
count = len(names)
`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})
	})

	Describe("map", func() {
		It("extracts values", func() {
			g := starlark.StringDict{"apps": apps}
			r := execScript(g, `
names = apps.map(lambda a: a.name)
first = names[0]
`)
			Expect(strVal(r, "first")).To(Equal("httpd"))
		})
	})

	Describe("any", func() {
		It("returns True when a match exists", func() {
			g := starlark.StringDict{"apps": apps}
			r := execScript(g, `found = apps.any(lambda a: "netscaler" in a.name)`)
			Expect(bool(r["found"].(starlark.Bool))).To(BeTrue())
		})

		It("returns False when no match exists", func() {
			g := starlark.StringDict{"apps": apps}
			r := execScript(g, `found = apps.any(lambda a: "oracle" in a.name)`)
			Expect(bool(r["found"].(starlark.Bool))).To(BeFalse())
		})

		It("returns False for empty list", func() {
			empty := NewApplicationList(nil)
			g := starlark.StringDict{"apps": empty}
			r := execScript(g, `found = apps.any(lambda a: True)`)
			Expect(bool(r["found"].(starlark.Bool))).To(BeFalse())
		})
	})

	Describe("on VirtualMachine", func() {
		It("exposes applications as ApplicationList", func() {
			vm := makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			vm.applications = NewApplicationList([]*Application{
				NewApplication(models.GuestApp{Name: "httpd", Version: "/usr/sbin/httpd"}),
			})
			g := starlark.StringDict{"vm": vm}
			r := execScript(g, `
count = len(vm.applications)
has_httpd = vm.applications.any(lambda a: a.name == "httpd")
has_oracle = vm.applications.any(lambda a: a.name == "oracle")
`)
			Expect(intVal(r, "count")).To(Equal(int64(1)))
			Expect(bool(r["has_httpd"].(starlark.Bool))).To(BeTrue())
			Expect(bool(r["has_oracle"].(starlark.Bool))).To(BeFalse())
		})
	})
})

var _ = Describe("GuestNetwork", func() {
	It("exposes all fields", func() {
		gn := NewGuestNetwork(models.GuestNetwork{
			Device: "eth0", MAC: "00:50:56:a1:b2:c3",
			IP: "10.0.0.5", PrefixLength: 24, Network: "VM Network",
		})
		g := starlark.StringDict{"gn": gn}
		r := execScript(g, `
device = gn.device
mac = gn.mac
ip = gn.ip
prefix_length = gn.prefix_length
network = gn.network
`)
		Expect(strVal(r, "device")).To(Equal("eth0"))
		Expect(strVal(r, "mac")).To(Equal("00:50:56:a1:b2:c3"))
		Expect(strVal(r, "ip")).To(Equal("10.0.0.5"))
		Expect(intVal(r, "prefix_length")).To(Equal(int64(24)))
		Expect(strVal(r, "network")).To(Equal("VM Network"))
	})
})
