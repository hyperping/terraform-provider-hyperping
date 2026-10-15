// Copyright (c) 2026 Hyperping
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	hyperping "github.com/hyperping/hyperping-go"
)

// ── unit ──

func TestStatusPageSSOConnectionResource_Metadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewStatusPageSSOConnectionResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "hyperping"}, resp)
	if resp.TypeName != "hyperping_statuspage_sso_connection" {
		t.Errorf("TypeName = %q", resp.TypeName)
	}

	dsResp := &datasource.MetadataResponse{}
	NewStatusPageSSOConnectionDataSource().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "hyperping"}, dsResp)
	if dsResp.TypeName != "hyperping_statuspage_sso_connection" {
		t.Errorf("data source TypeName = %q", dsResp.TypeName)
	}
}

func TestStatusPageSSOConnectionResource_Schema(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewStatusPageSSOConnectionResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	for _, name := range []string{"name", "issuer", "client_id", "client_secret"} {
		if !resp.Schema.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	if !resp.Schema.Attributes["client_secret"].IsSensitive() {
		t.Error("client_secret must be sensitive")
	}
	domains := resp.Schema.Attributes["allowed_domains"]
	if !domains.IsOptional() || !domains.IsComputed() {
		t.Error("allowed_domains must be Optional + Computed")
	}
	if !resp.Schema.Attributes["redirect_uri"].IsComputed() {
		t.Error("redirect_uri must be computed")
	}
}

func TestSameIssuer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://acme.eu.auth0.com/", "https://acme.eu.auth0.com/", true},
		{"https://acme.eu.auth0.com", "https://acme.eu.auth0.com/", true},
		{"acme.eu.auth0.com", "https://acme.eu.auth0.com/", true},
		{"https://ACME.eu.auth0.com", "https://acme.eu.auth0.com/", true},
		{"https://acme.eu.auth0.com/.well-known/openid-configuration", "https://acme.eu.auth0.com/", true},
		{"https://login.microsoftonline.com/tenant-1/v2.0", "https://login.microsoftonline.com/tenant-1/v2.0", true},
		{"https://login.microsoftonline.com/tenant-1/v2.0", "https://login.microsoftonline.com/tenant-2/v2.0", false},
		{"https://acme.okta.com/oauth2/default", "https://acme.okta.com", false},
		{"https://other.eu.auth0.com/", "https://acme.eu.auth0.com/", false},
		{"http://localhost:5500", "https://localhost:5500", false},
	}
	for _, c := range cases {
		if got := sameIssuer(c.a, c.b); got != c.want {
			t.Errorf("sameIssuer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// ── acceptance (mock API) ──

// mockSSOServer answers /v2/sso-connections like the Hyperping API: issuer
// stored as the IdP publishes it (base + "/", like Auth0), secret never
// returned, OIDC-only writes, delete refused while a page uses it.
type mockSSOServer struct {
	*httptest.Server
	t           *testing.T
	mu          sync.Mutex
	connections map[string]map[string]interface{}
	secrets     map[string]string
	order       []string
	counter     int
	puts        []map[string]interface{}
	usedByPage  map[string]bool
}

func newMockSSOServer(t *testing.T) *mockSSOServer {
	m := &mockSSOServer{t: t, connections: map[string]map[string]interface{}{}, secrets: map[string]string{}, usedByPage: map[string]bool{}}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *mockSSOServer) addSAML(uuid, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connections[uuid] = map[string]interface{}{
		"uuid": uuid, "name": name, "provider": "okta", "allowed_domains": []string{}, "active": true, "configured": true,
		"issuer": nil, "client_id": nil, "has_client_secret": false, "redirect_uri": nil,
	}
	m.order = append(m.order, uuid)
}

func storedIssuer(input string) string {
	return issuerBase(input) + "/"
}

func (m *mockSSOServer) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set(hyperping.HeaderContentType, hyperping.ContentTypeJSON)
	base := hyperping.SSOConnectionsBasePath
	id := strings.TrimPrefix(r.URL.Path, base+"/")
	reply := func(status int, body interface{}) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == base:
		list := make([]map[string]interface{}, 0, len(m.order))
		for _, uuid := range m.order {
			list = append(list, m.connections[uuid])
		}
		reply(http.StatusOK, map[string]interface{}{"connections": list})

	case r.Method == http.MethodPost && r.URL.Path == base:
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["client_secret"] != "secret-1" && req["client_secret"] != "secret-2" {
			reply(http.StatusBadRequest, map[string]string{"error": "The identity provider refused this client ID and secret (invalid_client).", "code": "client_refused"})
			return
		}
		m.counter++
		uuid := fmt.Sprintf("sso_mock%03d", m.counter)
		domains, ok := req["allowed_domains"].([]interface{})
		if !ok {
			domains = []interface{}{}
		}
		m.connections[uuid] = map[string]interface{}{
			"uuid": uuid, "name": req["name"], "provider": "oidc", "allowed_domains": domains, "active": true, "configured": true,
			"issuer": storedIssuer(req["issuer"].(string)), "client_id": req["client_id"], "has_client_secret": true,
			"redirect_uri": "https://api.hyperping.io/v1/auth/sso/oidc/callback",
		}
		m.secrets[uuid] = req["client_secret"].(string)
		m.order = append(m.order, uuid)
		reply(http.StatusCreated, map[string]interface{}{"message": "SSO connection created", "connection": m.connections[uuid]})

	case r.Method == http.MethodGet:
		c, ok := m.connections[id]
		if !ok {
			reply(http.StatusNotFound, map[string]string{"error": "SSO connection not found"})
			return
		}
		reply(http.StatusOK, map[string]interface{}{"connection": c})

	case r.Method == http.MethodPut:
		c, ok := m.connections[id]
		if !ok {
			reply(http.StatusNotFound, map[string]string{"error": "SSO connection not found"})
			return
		}
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.puts = append(m.puts, req)
		if secret, ok := req["client_secret"].(string); ok && secret != "secret-1" && secret != "secret-2" {
			reply(http.StatusBadRequest, map[string]string{"error": "The identity provider refused this client ID and secret (invalid_client)."})
			return
		}
		for _, field := range []string{"name", "client_id", "allowed_domains"} {
			if v, ok := req[field]; ok {
				c[field] = v
			}
		}
		if issuer, ok := req["issuer"].(string); ok {
			c["issuer"] = storedIssuer(issuer)
		}
		if secret, ok := req["client_secret"].(string); ok {
			m.secrets[id] = secret
		}
		reply(http.StatusOK, map[string]interface{}{"message": "SSO connection updated", "connection": c})

	case r.Method == http.MethodDelete:
		if _, ok := m.connections[id]; !ok {
			reply(http.StatusNotFound, map[string]string{"error": "SSO connection not found"})
			return
		}
		if m.usedByPage[id] {
			reply(http.StatusBadRequest, map[string]string{"error": "Cannot delete: 1 status page(s) are using this SSO connection. Remove SSO from those status pages first."})
			return
		}
		delete(m.connections, id)
		for i, uuid := range m.order {
			if uuid == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
		reply(http.StatusOK, map[string]string{"message": "SSO connection deleted"})

	default:
		reply(http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

func (m *mockSSOServer) lastPut() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.puts) == 0 {
		return nil
	}
	return m.puts[len(m.puts)-1]
}

func testAccSSOConnectionConfig(baseURL, name, issuer, secret, domains string) string {
	return testAccProviderConfig(baseURL) + fmt.Sprintf(`
resource "hyperping_statuspage_sso_connection" "auth0" {
  name          = %[1]q
  issuer        = %[2]q
  client_id     = "client-1"
  client_secret = %[3]q
  %[4]s
}
`, name, issuer, secret, domains)
}

func TestAccStatusPageSSOConnectionResource_lifecycle(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()
	const res = "hyperping_statuspage_sso_connection.auth0"

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			server.mu.Lock()
			defer server.mu.Unlock()
			if len(server.connections) != 0 {
				return fmt.Errorf("%d connection(s) left after destroy", len(server.connections))
			}
			return nil
		},
		Steps: []tfresource.TestStep{
			// Create, written as the IdP publishes the issuer.
			{
				Config: testAccSSOConnectionConfig(server.URL, "Auth0", "https://acme.eu.auth0.com/", "secret-1", `allowed_domains = ["acme.com"]`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(res, "id", "sso_mock001"),
					tfresource.TestCheckResourceAttr(res, "issuer", "https://acme.eu.auth0.com/"),
					tfresource.TestCheckResourceAttr(res, "client_id", "client-1"),
					tfresource.TestCheckResourceAttr(res, "client_secret", "secret-1"),
					tfresource.TestCheckResourceAttr(res, "allowed_domains.#", "1"),
					tfresource.TestCheckResourceAttr(res, "allowed_domains.0", "acme.com"),
					tfresource.TestCheckResourceAttr(res, "redirect_uri", "https://api.hyperping.io/v1/auth/sso/oidc/callback"),
				),
			},
			// Import: everything but the write-only secret.
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"client_secret"},
			},
			// Rename only: the IdP settings are not sent.
			{
				Config: testAccSSOConnectionConfig(server.URL, "Auth0 prod", "https://acme.eu.auth0.com/", "secret-1", `allowed_domains = ["acme.com"]`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(res, "name", "Auth0 prod"),
					func(_ *terraform.State) error {
						put := server.lastPut()
						if put["name"] != "Auth0 prod" {
							return fmt.Errorf("name not sent: %v", put)
						}
						for _, field := range []string{"issuer", "client_id", "client_secret", "allowed_domains"} {
							if _, sent := put[field]; sent {
								return fmt.Errorf("%s sent on a rename: %v", field, put)
							}
						}
						return nil
					},
				),
			},
			// Rotated secret: the three IdP settings go together; domains cleared.
			{
				Config: testAccSSOConnectionConfig(server.URL, "Auth0 prod", "https://acme.eu.auth0.com/", "secret-2", `allowed_domains = []`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(res, "client_secret", "secret-2"),
					tfresource.TestCheckResourceAttr(res, "allowed_domains.#", "0"),
					func(_ *terraform.State) error {
						put := server.lastPut()
						if put["client_secret"] != "secret-2" || put["client_id"] != "client-1" || put["issuer"] != "https://acme.eu.auth0.com/" {
							return fmt.Errorf("IdP settings not sent together: %v", put)
						}
						if domains, ok := put["allowed_domains"].([]interface{}); !ok || len(domains) != 0 {
							return fmt.Errorf("allowed_domains not cleared: %v", put)
						}
						return nil
					},
				),
			},
		},
	})
}

// The issuer as typed (no trailing slash, or a bare domain) keeps its spelling
// in state and plans no change, although the API stores "https://…/".
func TestAccStatusPageSSOConnectionResource_issuerAsTyped(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()
	const res = "hyperping_statuspage_sso_connection.auth0"

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: testAccSSOConnectionConfig(server.URL, "Auth0", "acme.eu.auth0.com", "secret-1", ""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(res, "issuer", "acme.eu.auth0.com"),
					// Omitted domains read back as the API's empty list.
					tfresource.TestCheckResourceAttr(res, "allowed_domains.#", "0"),
					func(_ *terraform.State) error {
						if got := server.connections["sso_mock001"]["issuer"]; got != "https://acme.eu.auth0.com/" {
							return fmt.Errorf("mock stored %v", got)
						}
						return nil
					},
				),
			},
			// Refreshed against the stored "https://acme.eu.auth0.com/": still no change.
			{
				Config:   testAccSSOConnectionConfig(server.URL, "Auth0", "acme.eu.auth0.com", "secret-1", ""),
				PlanOnly: true,
			},
		},
	})
}

func TestAccStatusPageSSOConnectionResource_refusedByIdP(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config:      testAccSSOConnectionConfig(server.URL, "Auth0", "https://acme.eu.auth0.com/", "wrong", ""),
				ExpectError: regexp.MustCompile(`refused this client ID and secret`),
			},
		},
	})
}

func TestAccStatusPageSSOConnectionResource_invalidDomain(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config:      testAccSSOConnectionConfig(server.URL, "Auth0", "https://acme.eu.auth0.com/", "secret-1", `allowed_domains = ["@acme.com"]`),
				ExpectError: regexp.MustCompile(`must be a domain`),
			},
		},
	})
}

func TestAccStatusPageSSOConnectionResource_importSAMLRefused(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()
	server.addSAML("sso_okta", "Okta")

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config:        testAccSSOConnectionConfig(server.URL, "Okta", "https://acme.okta.com", "secret-1", ""),
				ResourceName:  "hyperping_statuspage_sso_connection.auth0",
				ImportState:   true,
				ImportStateId: "sso_okta",
				ExpectError:   regexp.MustCompile(`Not an OpenID Connect connection`),
			},
		},
	})
}

func TestAccStatusPageSSOConnectionDataSource_byNameAndID(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()
	server.addSAML("sso_okta", "Okta")
	server.addSAML("sso_dup1", "Twin")
	server.addSAML("sso_dup2", "Twin")

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: testAccSSOConnectionConfig(server.URL, "Auth0", "https://acme.eu.auth0.com/", "secret-1", `allowed_domains = ["acme.com"]`) + `
data "hyperping_statuspage_sso_connection" "okta" {
  name = "Okta"
}

data "hyperping_statuspage_sso_connection" "auth0" {
  id = hyperping_statuspage_sso_connection.auth0.id
}
`,
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.okta", "id", "sso_okta"),
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.okta", "type", "okta"),
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.okta", "configured", "true"),
					tfresource.TestCheckNoResourceAttr("data.hyperping_statuspage_sso_connection.okta", "issuer"),
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.auth0", "type", "oidc"),
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.auth0", "issuer", "https://acme.eu.auth0.com/"),
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.auth0", "allowed_domains.0", "acme.com"),
					tfresource.TestCheckResourceAttr("data.hyperping_statuspage_sso_connection.auth0", "redirect_uri", "https://api.hyperping.io/v1/auth/sso/oidc/callback"),
				),
			},
			{
				Config: testAccProviderConfig(server.URL) + `
data "hyperping_statuspage_sso_connection" "twin" {
  name = "Twin"
}
`,
				ExpectError: regexp.MustCompile(`2 SSO connections are named "Twin"`),
			},
			{
				Config: testAccProviderConfig(server.URL) + `
data "hyperping_statuspage_sso_connection" "none" {
  name = "Nope"
}
`,
				ExpectError: regexp.MustCompile(`No SSO connection of the project is named "Nope"`),
			},
		},
	})
}

func TestAccStatusPageSSOConnectionResource_deleteRefusedWhileUsed(t *testing.T) {
	server := newMockSSOServer(t)
	defer server.Close()
	const res = "hyperping_statuspage_sso_connection.auth0"

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: testAccSSOConnectionConfig(server.URL, "Auth0", "https://acme.eu.auth0.com/", "secret-1", ""),
				Check: func(_ *terraform.State) error {
					server.mu.Lock()
					server.usedByPage["sso_mock001"] = true
					server.mu.Unlock()
					return nil
				},
			},
			{
				Config:      testAccProviderConfig(server.URL),
				ExpectError: regexp.MustCompile(`Cannot delete: 1 status page`),
			},
			{
				// Free it so the test can clean up.
				Config: testAccSSOConnectionConfig(server.URL, "Auth0", "https://acme.eu.auth0.com/", "secret-1", ""),
				Check: func(s *terraform.State) error {
					server.mu.Lock()
					server.usedByPage["sso_mock001"] = false
					server.mu.Unlock()
					return tfresource.TestCheckResourceAttr(res, "id", "sso_mock001")(s)
				},
			},
		},
	})
}
