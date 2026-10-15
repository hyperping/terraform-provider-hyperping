// Copyright (c) 2026 Hyperping
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	hyperping "github.com/hyperping/hyperping-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &StatusPageSSOConnectionResource{}
	_ resource.ResourceWithImportState = &StatusPageSSOConnectionResource{}
)

// ssoDomainPattern is the server's check on allowed_domains (it stores them as sent).
var ssoDomainPattern = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// NewStatusPageSSOConnectionResource creates a new status page SSO connection resource.
func NewStatusPageSSOConnectionResource() resource.Resource {
	return &StatusPageSSOConnectionResource{}
}

// StatusPageSSOConnectionResource manages an OpenID Connect connection that
// visitors of private status pages sign in with.
type StatusPageSSOConnectionResource struct {
	client hyperping.SSOConnectionAPI
}

// StatusPageSSOConnectionResourceModel describes the resource data model.
type StatusPageSSOConnectionResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Issuer         types.String `tfsdk:"issuer"`
	ClientID       types.String `tfsdk:"client_id"`
	ClientSecret   types.String `tfsdk:"client_secret"`
	AllowedDomains types.List   `tfsdk:"allowed_domains"`
	RedirectURI    types.String `tfsdk:"redirect_uri"`
}

// Metadata returns the resource type name.
func (r *StatusPageSSOConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage_sso_connection"
}

// Schema defines the schema for the resource.
func (r *StatusPageSSOConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an OpenID Connect SSO connection that visitors of private status pages sign in with " +
			"(Auth0, Okta, Microsoft Entra ID, Keycloak…). A status page uses it through " +
			"`settings.authentication.sso_connection_uuid`.\n\n" +
			"Hyperping checks the issuer's discovery document and the client credentials before saving: a configuration " +
			"the identity provider refuses fails at `apply` and creates nothing. Register `redirect_uri` as an allowed " +
			"callback URL in the identity provider. A connection is billed like one created in the dashboard. " +
			"SAML connections are set up in the dashboard; read them with the `hyperping_statuspage_sso_connection` data source.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Connection ID (`sso_…`). Set it as `settings.authentication.sso_connection_uuid` of a `hyperping_statuspage`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connection, shown in the dashboard.",
				Required:            true,
				Validators: []validator.String{
					StringLength(1, 255),
				},
			},
			"issuer": schema.StringAttribute{
				MarkdownDescription: "Issuer URL of the identity provider: the address before `/.well-known/openid-configuration` " +
					"(e.g. `https://acme.eu.auth0.com/`). A bare domain also works. Hyperping stores the issuer the identity " +
					"provider publishes; the configuration keeps its own spelling as long as it names the same identity provider " +
					"(trailing slash, `https://`, discovery path). After `terraform import` the issuer reads back as stored: write it " +
					"that way, or the first `apply` makes an update that changes nothing on the Hyperping side.",
				Required: true,
				Validators: []validator.String{
					StringLength(1, 2048),
				},
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Client ID of the application registered in the identity provider.",
				Required:            true,
				Validators: []validator.String{
					StringLength(1, 255),
					stringvalidator.RegexMatches(regexp.MustCompile(`^\S+$`), "must not contain whitespace"),
				},
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Client secret of the application. Write-only on the API side: Hyperping never returns it, " +
					"so a secret rotated outside Terraform is not detected. After `terraform import`, the next `apply` sends it once.",
				Required:  true,
				Sensitive: true,
				Validators: []validator.String{
					StringLength(1, 2048),
				},
			},
			"allowed_domains": schema.ListAttribute{
				MarkdownDescription: "Email domains allowed to sign in (e.g. `[\"acme.com\"]`). Empty = any account the identity provider signs in. " +
					"When omitted, the domains set in the dashboard are kept.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(100),
					listvalidator.ValueStringsAre(stringvalidator.RegexMatches(ssoDomainPattern, "must be a domain, e.g. acme.com")),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"redirect_uri": schema.StringAttribute{
				MarkdownDescription: "Callback URL to register in the identity provider (`https://api.hyperping.io/v1/auth/sso/oidc/callback`, " +
					"or `https://api.eu.hyperping.io/...` for EU accounts).",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *StatusPageSSOConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clients, ok := req.ProviderData.(*hyperpingClients)
	if !ok {
		resp.Diagnostics.Append(newUnexpectedConfigTypeError("*hyperpingClients", req.ProviderData))
		return
	}

	r.client = clients.REST
}

// Create creates the resource and sets the initial Terraform state.
func (r *StatusPageSSOConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageSSOConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := hyperping.CreateSSOConnectionRequest{
		Name:         plan.Name.ValueString(),
		Provider:     hyperping.SSOConnectionProviderOIDC,
		Issuer:       plan.Issuer.ValueString(),
		ClientID:     plan.ClientID.ValueString(),
		ClientSecret: plan.ClientSecret.ValueString(),
	}
	if !isNullOrUnknown(plan.AllowedDomains) {
		resp.Diagnostics.Append(plan.AllowedDomains.ElementsAs(ctx, &createReq.AllowedDomains, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	connection, err := r.client.CreateSSOConnection(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating status page SSO connection",
			fmt.Sprintf("Could not create the SSO connection: %s", err),
		)
		return
	}

	applySSOConnection(connection, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *StatusPageSSOConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageSSOConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	connection, err := r.client.GetSSOConnection(ctx, state.ID.ValueString())
	if err != nil {
		if hyperping.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error reading status page SSO connection",
			fmt.Sprintf("Could not read SSO connection %s: %s", state.ID.ValueString(), err),
		)
		return
	}
	if connection.Provider != hyperping.SSOConnectionProviderOIDC {
		resp.Diagnostics.AddError(
			"Not an OpenID Connect connection",
			fmt.Sprintf("SSO connection %s uses %q. This resource manages OpenID Connect connections only: "+
				"manage this one in the dashboard and read it with the hyperping_statuspage_sso_connection data source.",
				connection.UUID, connection.Provider),
		)
		return
	}

	applySSOConnection(connection, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the resource and sets the updated Terraform state.
func (r *StatusPageSSOConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state StatusPageSSOConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := hyperping.UpdateSSOConnectionRequest{}
	if !plan.Name.Equal(state.Name) {
		updateReq.Name = tfStringToPtr(plan.Name)
	}
	// The identity provider settings go together: the API checks them as a set
	// (a new client ID needs its secret) and skips the check when they are the
	// ones already saved.
	if !plan.Issuer.Equal(state.Issuer) || !plan.ClientID.Equal(state.ClientID) || !plan.ClientSecret.Equal(state.ClientSecret) {
		updateReq.Issuer = tfStringToPtr(plan.Issuer)
		updateReq.ClientID = tfStringToPtr(plan.ClientID)
		updateReq.ClientSecret = tfStringToPtr(plan.ClientSecret)
	}
	if !isNullOrUnknown(plan.AllowedDomains) && !plan.AllowedDomains.Equal(state.AllowedDomains) {
		domains := []string{}
		resp.Diagnostics.Append(plan.AllowedDomains.ElementsAs(ctx, &domains, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		updateReq.AllowedDomains = &domains
	}

	connection, err := r.client.UpdateSSOConnection(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating status page SSO connection",
			fmt.Sprintf("Could not update SSO connection %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	plan.ID = state.ID
	applySSOConnection(connection, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *StatusPageSSOConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state StatusPageSSOConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSSOConnection(ctx, state.ID.ValueString()); err != nil {
		if hyperping.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Error deleting status page SSO connection",
			fmt.Sprintf("Could not delete SSO connection %s: %s", state.ID.ValueString(), err),
		)
	}
}

// ImportState imports an existing OpenID Connect connection by its ID (sso_…).
func (r *StatusPageSSOConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applySSOConnection maps the API's answer onto the model. The client secret
// is never returned, so the model keeps its own. The issuer keeps the value
// written in the configuration while it names the same identity provider as
// the one Hyperping stored (see sameIssuer).
func applySSOConnection(connection *hyperping.SSOConnection, model *StatusPageSSOConnectionResourceModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(connection.UUID)
	model.Name = types.StringValue(connection.Name)

	switch {
	case connection.Issuer == nil:
		model.Issuer = types.StringNull()
	case isNullOrUnknown(model.Issuer) || !sameIssuer(model.Issuer.ValueString(), *connection.Issuer):
		model.Issuer = types.StringValue(*connection.Issuer)
	}

	model.ClientID = stringPtrToTF(connection.ClientID)
	model.RedirectURI = stringPtrToTF(connection.RedirectURI)
	model.AllowedDomains = stringsToList(connection.AllowedDomains, diags)
}

// sameIssuer mirrors issuerBaseFrom in the Hyperping API: a bare domain means
// https, and the discovery path and trailing slashes are not part of the
// issuer ("acme.eu.auth0.com" and "https://acme.eu.auth0.com/" are one IdP).
func sameIssuer(a, b string) bool {
	return issuerBase(a) == issuerBase(b)
}

func issuerBase(input string) string {
	text := strings.TrimSpace(input)
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}
	u, err := url.Parse(text)
	if err != nil || u.Host == "" {
		return strings.TrimRight(text, "/")
	}
	p := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/.well-known/openid-configuration")
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(p, "/")
}

// stringsToList converts a slice to a list, empty rather than null: an empty
// allowed_domains written in the configuration must read back as written.
func stringsToList(values []string, diags *diag.Diagnostics) types.List {
	elements := make([]attr.Value, len(values))
	for i, v := range values {
		elements[i] = types.StringValue(v)
	}
	list, listDiags := types.ListValue(types.StringType, elements)
	diags.Append(listDiags...)
	return list
}

// stringPtrToTF converts *string to types.String (null when nil).
func stringPtrToTF(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}
