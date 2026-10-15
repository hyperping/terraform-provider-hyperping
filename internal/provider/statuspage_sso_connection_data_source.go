// Copyright (c) 2026 Hyperping
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	hyperping "github.com/hyperping/hyperping-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ datasource.DataSource              = &StatusPageSSOConnectionDataSource{}
	_ datasource.DataSourceWithConfigure = &StatusPageSSOConnectionDataSource{}
)

// NewStatusPageSSOConnectionDataSource creates a new status page SSO connection data source.
func NewStatusPageSSOConnectionDataSource() datasource.DataSource {
	return &StatusPageSSOConnectionDataSource{}
}

// StatusPageSSOConnectionDataSource reads one SSO connection of the project,
// whatever its provider (OpenID Connect, SAML or WorkOS).
type StatusPageSSOConnectionDataSource struct {
	client hyperping.SSOConnectionAPI
}

// StatusPageSSOConnectionDataSourceModel describes the data source data model.
type StatusPageSSOConnectionDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Type           types.String `tfsdk:"type"`
	Configured     types.Bool   `tfsdk:"configured"`
	AllowedDomains types.List   `tfsdk:"allowed_domains"`
	Issuer         types.String `tfsdk:"issuer"`
	ClientID       types.String `tfsdk:"client_id"`
	RedirectURI    types.String `tfsdk:"redirect_uri"`
}

// Metadata returns the data source type name.
func (d *StatusPageSSOConnectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage_sso_connection"
}

// Schema defines the schema for the data source.
func (d *StatusPageSSOConnectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a status page SSO connection of the project by ID or name, including SAML connections set up in the dashboard. " +
			"Use its `id` as `settings.authentication.sso_connection_uuid` of a `hyperping_statuspage`.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Connection ID (`sso_…`).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connection to look up. It must match exactly one connection of the project.",
				Optional:            true,
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Identity provider type: `oidc` (OpenID Connect), `okta`, `azure`, `google` or `saml` (SAML), or `workos`.",
				Computed:            true,
			},
			"configured": schema.BoolAttribute{
				MarkdownDescription: "Whether the identity provider settings are saved.",
				Computed:            true,
			},
			"allowed_domains": schema.ListAttribute{
				MarkdownDescription: "Email domains allowed to sign in. Empty = any.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"issuer": schema.StringAttribute{
				MarkdownDescription: "OpenID Connect issuer (null for other types).",
				Computed:            true,
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "OpenID Connect client ID (null for other types).",
				Computed:            true,
			},
			"redirect_uri": schema.StringAttribute{
				MarkdownDescription: "OpenID Connect callback URL to register in the identity provider (null for other types).",
				Computed:            true,
			},
		},
	}
}

// Configure adds the provider configured client to the data source.
func (d *StatusPageSSOConnectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clients, ok := req.ProviderData.(*hyperpingClients)
	if !ok {
		resp.Diagnostics.Append(newUnexpectedConfigTypeError("*hyperpingClients", req.ProviderData))
		return
	}

	d.client = clients.REST
}

// Read refreshes the Terraform state with the latest data.
func (d *StatusPageSSOConnectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data StatusPageSSOConnectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var found *hyperping.SSOConnection
	if !data.ID.IsNull() {
		connection, err := d.client.GetSSOConnection(ctx, data.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error reading status page SSO connection",
				fmt.Sprintf("Could not read SSO connection %s: %s", data.ID.ValueString(), err),
			)
			return
		}
		found = connection
	} else {
		connections, err := d.client.ListSSOConnections(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Error listing status page SSO connections", err.Error())
			return
		}
		name := data.Name.ValueString()
		var matches []hyperping.SSOConnection
		for _, c := range connections {
			if c.Name == name {
				matches = append(matches, c)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("SSO connection not found", fmt.Sprintf("No SSO connection of the project is named %q.", name))
			return
		case 1:
			found = &matches[0]
		default:
			ids := make([]string, len(matches))
			for i, m := range matches {
				ids[i] = m.UUID
			}
			resp.Diagnostics.AddError(
				"Several SSO connections match",
				fmt.Sprintf("%d SSO connections are named %q (%v): look it up by id instead.", len(matches), name, ids),
			)
			return
		}
	}

	data.ID = types.StringValue(found.UUID)
	data.Name = types.StringValue(found.Name)
	data.Type = types.StringValue(found.Provider)
	data.Configured = types.BoolValue(found.Configured)
	data.AllowedDomains = stringsToList(found.AllowedDomains, &resp.Diagnostics)
	data.Issuer = stringPtrToTF(found.Issuer)
	data.ClientID = stringPtrToTF(found.ClientID)
	data.RedirectURI = stringPtrToTF(found.RedirectURI)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
