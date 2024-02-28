package platform

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/jfrog/terraform-provider-shared/util"
	utilfw "github.com/jfrog/terraform-provider-shared/util/fw"
	"github.com/samber/lo"
)

const PermissionEndpoint = "/access/api/v2/permissions"

var _ resource.Resource = (*permissionResource)(nil)

type permissionResource struct {
	ProviderData util.ProvderMetadata
}

func NewPermissionResource() resource.Resource {
	return &permissionResource{}
}

func (r *permissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission"
}

var usersGroupsAttributeSchema = func(description string) schema.SetNestedAttribute {
	return schema.SetNestedAttribute{
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Required: true,
				},
				"permissions": schema.SetAttribute{
					ElementType: types.StringType,
					Required:    true,
					Validators: []validator.Set{
						setvalidator.ValueStringsAre(stringvalidator.OneOf([]string{"WRITE", "MANAGE", "SCAN", "DELETE", "READ", "ANNOTATE", "EXECUTE"}...)),
					},
					PlanModifiers: []planmodifier.Set{
						setplanmodifier.UseStateForUnknown(),
					},
					MarkdownDescription: description,
				},
			},
		},
		Optional: true,
		PlanModifiers: []planmodifier.Set{
			setplanmodifier.UseStateForUnknown(),
		},
	}
}

var actionsAttributeSchema = func(description string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"users":  usersGroupsAttributeSchema(description),
			"groups": usersGroupsAttributeSchema(description),
		},
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
	}
}

var targetAttributeSchema = func(isBuild bool, nameDescription, includeDescription, excludeDescription string) schema.SetNestedAttribute {
	nameValidators := []validator.String{}
	if isBuild {
		nameValidators = append(nameValidators, stringvalidator.OneOf([]string{"artifactory-build-info"}...))
	}

	attr := schema.SetNestedAttribute{
		Required: true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Required:            true,
					Validators:          nameValidators,
					MarkdownDescription: nameDescription,
				},
				"include_patterns": schema.SetAttribute{
					ElementType: types.StringType,
					Optional:    true,
					Computed:    true,
					Default: setdefault.StaticValue(
						types.SetValueMust(types.StringType, []attr.Value{types.StringValue("**")}),
					),
					Validators: []validator.Set{
						setvalidator.SizeAtLeast(1),
					},
					PlanModifiers: []planmodifier.Set{
						setplanmodifier.UseStateForUnknown(),
					},
					MarkdownDescription: includeDescription,
				},
				"exclude_patterns": schema.SetAttribute{
					ElementType: types.StringType,
					Optional:    true,
					Validators: []validator.Set{
						setvalidator.SizeAtLeast(1),
					},
					PlanModifiers: []planmodifier.Set{
						setplanmodifier.UseStateForUnknown(),
					},
					MarkdownDescription: excludeDescription,
				},
			},
		},
	}

	if isBuild {
		attr.Validators = []validator.Set{
			setvalidator.SizeAtMost(1),
		}
	}

	return attr
}

func (r *permissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
				Description: "Permission name",
			},
			"artifact": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"actions": actionsAttributeSchema(
						"**READ**: Downloads artifacts and reads the metadata.\n" +
							"**ANNOTATE**: Annotates artifacts and folders with metadata and properties.\n" +
							"**WRITE**: Deploys artifacts & deploys to remote repository caches.\n" +
							"**DELETE**: Deletes or overwrites artifacts.\n" +
							"**SCAN**: Triggers Xray scans on artifacts in repositories. Creates and deletes custom issues and license.\n" +
							"**MANAGE**: Allows changing the permission settings for other users in this permission target. It does not permit adding/removing resources to the permission target.",
					),
					"targets": targetAttributeSchema(
						false,
						"Specify repository key as name. Use `ANY_LOCAL`, `ANY_REMOTE`, or `ANY_DISTRIBUTION` for any repository type.",
						"Simple comma separated wildcard patterns for **existing and future** repository artifact paths (with no leading slash). Ant-style path expressions are supported (*, **, ?). For example: `org/apache/**`",
						"Simple comma separated wildcard patterns for **existing and future** repository artifact paths (with no leading slash). Ant-style path expressions are supported (*, **, ?). For example: `org/apache/**`",
					),
				},
				Description: "Defines the repositories to be used or excluded.",
			},
			"build": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"actions": actionsAttributeSchema(
						"**READ**: View and downloads build info artifacts from the artifactory-build-info default repository and reads the corresponding build in the Builds page.\n" +
							"**ANNOTATE**: Annotates build info artifacts and folders with metadata and properties.\n" +
							"**WRITE**: Allows uploading and promoting build info artifacts.\n" +
							"**DELETE**: Deletes build info artifacts.\n" +
							"**SCAN**: Triggers Xray scans on builds. Creates and deletes custom issues and license.\n" +
							"**MANAGE**: Allows changing build info permission settings for other users in this permission target. It does not permit adding/removing resources to the permission target.",
					),
					"targets": targetAttributeSchema(
						true,
						"Only `artifactory-build-info` is allowed for name. Specify build name as part of the `include_patterns` or `exclude_patterns`.",
						"Use Ant-style wildcard patterns to specify **existing and future** build names (i.e. artifact paths) in the build info repository (without a leading slash) that will be included in this permission target. Ant-style path expressions are supported (*, **, ?). For example, an `apache/**` pattern will include the \"apache\" build info in the permission.",
						"Use Ant-style wildcard patterns to specify **existing and future** build names (i.e. artifact paths) in the build info repository (without a leading slash) that will be excluded from this permission target. Ant-style path expressions are supported (*, **, ?). For example, an `apache/**` pattern will exclude the \"apache\" build info from the permission.",
					),
				},
				Description: "Defines the builds to be used or excluded.",
			},
			"release_bundle": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"actions": actionsAttributeSchema(
						"**READ**: Views and downloads Release Bundle artifacts from the relevant Release Bundle repository and reads the corresponding Release Bundles in the Distribution page.\n" +
							"**ANNOTATE**: Annotates Release Bundle artifacts and folder with metadata and properties.\n" +
							"**WRITE**: Creates Release Bundles.\n" +
							"**EXECUTE**: Allows users to promote Release Bundles v2 to a selected target environment and is a prerequisite for distributing Release Bundles (v1 & v2) to Distribution Edge nodes.\n" +
							"**DELETE**: Deletes Release Bundles.\n" +
							"**SCAN** Xray Metadata: Triggers Xray scans on Release Bundles. Creates and deletes custom issues and license.\n" +
							"**MANAGE**: Allows changing Release Bundle permission settings for other users in this permission target. It does not permit adding/removing resources to the permission target.",
					),
					"targets": targetAttributeSchema(
						false,
						"Specify release bundle repository key as name.",
						"Simple wildcard patterns for **existing and future** Release Bundle names. Ant-style path expressions are supported (*, **, ?). For example: `product_*/**`",
						"Simple wildcard patterns for **existing and future** Release Bundle names. Ant-style path expressions are supported (*, **, ?). For example: `product_*/**`",
					),
				},
				Description: "Defines the release bundles to be used or excluded.",
			},
			"destination": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"actions": actionsAttributeSchema(
						"**EXECUTE**: Distributes Release Bundles according to their destination permissions.\n" +
							"**DELETE**: Deletes Release Bundles from the selected destinations.\n" +
							"**MANAGE**: Adds and deletes users who can distribute Release Bundles on assigned destinations.",
					),
					"targets": targetAttributeSchema(
						false,
						"Specify destination name as name. Use `*` to include all destinations.",
						"Simple wildcard patterns for existing and future JPD or city names. Ant-style path expressions are supported (*, **, ?). For example: `site_*` or `New*`",
						"Simple wildcard patterns for existing and future JPD or city names. Ant-style path expressions are supported (*, **, ?). For example: `site_*` or `New*`",
					),
				},
				Description: "Defines the destinations to be used or excluded.",
			},
			"pipeline_source": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"actions": actionsAttributeSchema(
						"**READ**: View the available pipeline sources.\n" +
							"**EXECUTE**: Manually trigger execution of steps.\n" +
							"**MANAGE**: Create and edit pipeline sources.",
					),
					"targets": targetAttributeSchema(
						false,
						"Specify pipeline source name as name. Use `*` to include all pipeline sources.",
						"Use Ant-style wildcard patterns to specify the full repository name of the **existing and future** pipeline sources that will be included in this permission. The pattern should have the following format: `{FULL_REPOSITORY_NAME_PATTERN}/**`. Ant-style path expressions are supported (*, **, ?). For example, the pattern `*/*test*/**` will include all repositories that contain the word \"test\" regardless of the repository owner.",
						"Use Ant-style wildcard patterns to specify the full repository name of the **existing and future** pipeline sources that will be excluded from this permission. The pattern should have the following format: `{FULL_REPOSITORY_NAME_PATTERN}/**`. Ant-style path expressions are supported (*, **, ?). For example, the pattern `*/*test*/**` will exclude all repositories that contain the word \"test\" regardless of the repository owner.",
					),
				},
				Description: "Defines the pipeline sources to be used or excluded.",
			},
		},
		MarkdownDescription: "Provides a JFrog [permission](https://jfrog.com/help/r/jfrog-platform-administration-documentation/permissions) resource to manage how users and groups access JFrog resources. This resource is applicable for the next-generation permissions model and fully backwards compatible with the legacy `artifactory_permission_target` resource in Artifactory provider.",
	}
}

func (r permissionResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.AtLeastOneOf(
			path.MatchRoot("artifact"),
			path.MatchRoot("build"),
			path.MatchRoot("release_bundle"),
			path.MatchRoot("destination"),
			path.MatchRoot("pipeline_source"),
		),
	}
}

type permissionResourceModel struct {
	Name           types.String `tfsdk:"name"`
	Artifact       types.Object `tfsdk:"artifact"`
	Build          types.Object `tfsdk:"build"`
	ReleaseBundle  types.Object `tfsdk:"release_bundle"`
	Destination    types.Object `tfsdk:"destination"`
	PipelineSource types.Object `tfsdk:"pipeline_source"`
}

type permissionActionsTargetsResourceModel struct {
	Actions types.Object `tfsdk:"actions"`
	Targets types.Set    `tfsdk:"targets"`
}

type permissionActionsResourceModel struct {
	Users  types.Set `tfsdk:"users"`
	Groups types.Set `tfsdk:"groups"`
}

type permissionUsersGroupsResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Permissions types.Set    `tfsdk:"permissions"`
}

type permissionTargetsResourceModel struct {
	Name            types.String `tfsdk:"name"`
	IncludePatterns types.Set    `tfsdk:"include_patterns"`
	ExcludePatterns types.Set    `tfsdk:"exclude_patterns"`
}

func (r *permissionResourceModel) toResourceAPIModel(ctx context.Context, tfResource types.Object, apiResource *permissionActionsTargetsAPIModel) (ds diag.Diagnostics) {
	var resource permissionActionsTargetsResourceModel
	ds.Append(tfResource.As(ctx, &resource, basetypes.ObjectAsOptions{})...)
	if ds.HasError() {
		return
	}

	actions := permissionActionsAPIModel{}
	if !resource.Actions.IsNull() {
		var actionsResource permissionActionsResourceModel
		ds.Append(resource.Actions.As(ctx, &actionsResource, basetypes.ObjectAsOptions{})...)
		if ds.HasError() {
			return
		}

		users := make(map[string][]string)
		if !actionsResource.Users.IsNull() {
			var usersResources []permissionUsersGroupsResourceModel
			ds.Append(actionsResource.Users.ElementsAs(ctx, &usersResources, false)...)
			if ds.HasError() {
				return
			}
			for _, us := range usersResources {
				var permissions []string
				us.Permissions.ElementsAs(ctx, &permissions, false)
				users[us.Name.ValueString()] = permissions
			}
		}

		groups := make(map[string][]string)
		if !actionsResource.Groups.IsNull() {
			var groupsResources []permissionUsersGroupsResourceModel
			ds.Append(actionsResource.Groups.ElementsAs(ctx, &groupsResources, false)...)
			if ds.HasError() {
				return
			}
			for _, gs := range groupsResources {
				var permissions []string
				gs.Permissions.ElementsAs(ctx, &permissions, false)
				groups[gs.Name.ValueString()] = permissions
			}
		}

		actions = permissionActionsAPIModel{
			Users:  users,
			Groups: groups,
		}
	}

	var targetsResource []permissionTargetsResourceModel
	ds.Append(resource.Targets.ElementsAs(ctx, &targetsResource, false)...)
	if ds.HasError() {
		return
	}

	targets := map[string]permissionTargetsAPIModel{}
	for _, tr := range targetsResource {
		var includePatterns []string
		var excludePatterns []string
		tr.IncludePatterns.ElementsAs(ctx, &includePatterns, false)
		tr.ExcludePatterns.ElementsAs(ctx, &excludePatterns, false)

		targets[tr.Name.ValueString()] = permissionTargetsAPIModel{
			IncludePatterns: includePatterns,
			ExcludePatterns: excludePatterns,
		}
	}

	*apiResource = permissionActionsTargetsAPIModel{
		Actions: &actions,
		Targets: targets,
	}

	return
}

func (r *permissionResourceModel) toAPIModel(ctx context.Context, apiModel *PermissionAPIModel) (ds diag.Diagnostics) {
	resources := make(map[string]*permissionActionsTargetsAPIModel)

	if !r.Artifact.IsNull() {
		var artifact permissionActionsTargetsAPIModel
		r.toResourceAPIModel(ctx, r.Artifact, &artifact)
		resources["artifact"] = &artifact
	}

	if !r.Build.IsNull() {
		var build permissionActionsTargetsAPIModel
		r.toResourceAPIModel(ctx, r.Build, &build)
		resources["build"] = &build
	}

	if !r.ReleaseBundle.IsNull() {
		var releaseBundle permissionActionsTargetsAPIModel
		r.toResourceAPIModel(ctx, r.ReleaseBundle, &releaseBundle)
		resources["release_bundle"] = &releaseBundle
	}

	if !r.Destination.IsNull() {
		var destination permissionActionsTargetsAPIModel
		r.toResourceAPIModel(ctx, r.Destination, &destination)
		resources["destination"] = &destination
	}

	if !r.PipelineSource.IsNull() {
		var pipelineSource permissionActionsTargetsAPIModel
		r.toResourceAPIModel(ctx, r.PipelineSource, &pipelineSource)
		resources["pipeline_source"] = &pipelineSource
	}

	*apiModel = PermissionAPIModel{
		Name:      r.Name.ValueString(),
		Resources: resources,
	}

	return nil
}

var usersGroupsResourceModelAttributeTypes types.ObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"name":        types.StringType,
		"permissions": types.SetType{ElemType: types.StringType},
	},
}

var actionsResourceModelAttributeTypes map[string]attr.Type = map[string]attr.Type{
	"users":  types.SetType{ElemType: usersGroupsResourceModelAttributeTypes},
	"groups": types.SetType{ElemType: usersGroupsResourceModelAttributeTypes},
}

var targetResourceModelAttributeType map[string]attr.Type = map[string]attr.Type{
	"name":             types.StringType,
	"include_patterns": types.SetType{ElemType: types.StringType},
	"exclude_patterns": types.SetType{ElemType: types.StringType},
}

var targetsResourceModelAttributeTypes types.ObjectType = types.ObjectType{
	AttrTypes: targetResourceModelAttributeType,
}

var resourceResourceModelAttributeTypes map[string]attr.Type = map[string]attr.Type{
	"actions": types.ObjectType{
		AttrTypes: actionsResourceModelAttributeTypes,
	},
	"targets": types.SetType{
		ElemType: targetsResourceModelAttributeTypes,
	},
}

func (r *permissionResourceModel) fromUsersGroupsAPIModel(ctx context.Context, usersGroups map[string][]string) (set types.Set, ds diag.Diagnostics) {
	set = types.SetNull(usersGroupsResourceModelAttributeTypes)
	if len(usersGroups) == 0 {
		return
	}

	userGroupSet := lo.MapToSlice(
		usersGroups,
		func(name string, permissions []string) attr.Value {
			ps := types.SetNull(types.StringType)
			if len(permissions) > 0 {
				p, d := types.SetValueFrom(ctx, types.StringType, permissions)
				if d != nil {
					ds = append(ds, d...)
				}
				ps = p
			}

			t, d := types.ObjectValue(
				map[string]attr.Type{
					"name":        types.StringType,
					"permissions": types.SetType{ElemType: types.StringType},
				},
				map[string]attr.Value{
					"name":        types.StringValue(name),
					"permissions": ps,
				},
			)
			if d != nil {
				ds = append(ds, d...)
			}

			return t
		},
	)

	ugs, d := types.SetValue(
		usersGroupsResourceModelAttributeTypes,
		userGroupSet,
	)
	if d != nil {
		ds = append(ds, d...)
	}
	set = ugs

	return
}

func (r *permissionResourceModel) fromResourceAPIModel(ctx context.Context, resourceAPIModel *permissionActionsTargetsAPIModel) (obj basetypes.ObjectValue, ds diag.Diagnostics) {
	if resourceAPIModel == nil {
		obj = types.ObjectNull(resourceResourceModelAttributeTypes)
		return
	}

	actions := types.ObjectNull(actionsResourceModelAttributeTypes)
	if resourceAPIModel.Actions != nil &&
		(len(resourceAPIModel.Actions.Users) > 0 || len(resourceAPIModel.Actions.Groups) > 0) {

		users, d := r.fromUsersGroupsAPIModel(ctx, resourceAPIModel.Actions.Users)
		if d != nil {
			ds = append(ds, d...)
		}

		groups, d := r.fromUsersGroupsAPIModel(ctx, resourceAPIModel.Actions.Groups)
		if d != nil {
			ds = append(ds, d...)
		}

		as, d := types.ObjectValue(
			actionsResourceModelAttributeTypes,
			map[string]attr.Value{
				"users":  users,
				"groups": groups,
			},
		)
		if d != nil {
			ds = append(ds, d...)
		}
		actions = as
	}

	targets := types.SetNull(targetsResourceModelAttributeTypes)

	if len(resourceAPIModel.Targets) > 0 {
		targetSet := lo.MapToSlice(
			resourceAPIModel.Targets,
			func(name string, filter permissionTargetsAPIModel) attr.Value {
				includePatterns := types.SetNull(types.StringType)
				if len(filter.IncludePatterns) > 0 {
					s, d := types.SetValueFrom(ctx, types.StringType, filter.IncludePatterns)
					if d != nil {
						ds = append(ds, d...)
					}
					includePatterns = s
				}

				excludePatterns := types.SetNull(types.StringType)
				if len(filter.ExcludePatterns) > 0 {
					s, d := types.SetValueFrom(ctx, types.StringType, filter.ExcludePatterns)
					if d != nil {
						ds = append(ds, d...)
					}
					excludePatterns = s

				}
				t, d := types.ObjectValue(
					targetResourceModelAttributeType,
					map[string]attr.Value{
						"name":             types.StringValue(name),
						"include_patterns": includePatterns,
						"exclude_patterns": excludePatterns,
					},
				)
				if d != nil {
					ds = append(ds, d...)
				}

				return t
			},
		)

		ts, d := types.SetValue(
			targetsResourceModelAttributeTypes,
			targetSet,
		)
		if d != nil {
			ds = append(ds, d...)
		}
		targets = ts
	}

	obj, d := types.ObjectValue(
		resourceResourceModelAttributeTypes,
		map[string]attr.Value{
			"actions": actions,
			"targets": targets,
		},
	)
	if d != nil {
		ds = append(ds, d...)
	}

	return
}

func (r *permissionResourceModel) fromAPIModel(ctx context.Context, apiModel *PermissionAPIModel) (ds diag.Diagnostics) {
	r.Name = types.StringValue(apiModel.Name)

	artifact, d := r.fromResourceAPIModel(ctx, apiModel.Resources["artifact"])
	if d != nil {
		ds = append(ds, d...)
	}
	r.Artifact = artifact

	build, d := r.fromResourceAPIModel(ctx, apiModel.Resources["build"])
	if d != nil {
		ds = append(ds, d...)
	}
	r.Build = build

	releaseBundle, d := r.fromResourceAPIModel(ctx, apiModel.Resources["release_bundle"])
	if d != nil {
		ds = append(ds, d...)
	}
	r.ReleaseBundle = releaseBundle

	destination, d := r.fromResourceAPIModel(ctx, apiModel.Resources["destination"])
	if d != nil {
		ds = append(ds, d...)
	}
	r.Destination = destination

	pipelineSource, d := r.fromResourceAPIModel(ctx, apiModel.Resources["pipeline_source"])
	if d != nil {
		ds = append(ds, d...)
	}
	r.PipelineSource = pipelineSource

	return
}

type PermissionAPIModel struct {
	Name      string                                       `json:"name"`
	Resources map[string]*permissionActionsTargetsAPIModel `json:"resources"`
}

type permissionActionsTargetsAPIModel struct {
	Actions *permissionActionsAPIModel           `json:"actions"`
	Targets map[string]permissionTargetsAPIModel `json:"targets"`
}

type permissionActionsAPIModel struct {
	Users  map[string][]string `json:"users"`
	Groups map[string][]string `json:"groups"`
}

type permissionTargetsAPIModel struct {
	IncludePatterns []string `tfsdk:"include_patterns" json:"include_patterns"`
	ExcludePatterns []string `tfsdk:"exclude_patterns" json:"exclude_patterns"`
}

func (r *permissionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	r.ProviderData = req.ProviderData.(util.ProvderMetadata)
}

func (r *permissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan permissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var permission PermissionAPIModel
	resp.Diagnostics.Append(plan.toAPIModel(ctx, &permission)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := r.ProviderData.Client.R().
		SetBody(&permission).
		Post(PermissionEndpoint)
	if err != nil {
		utilfw.UnableToCreateResourceError(resp, err.Error())
		return
	}

	// Return error if the HTTP status code is not 201 Created
	if response.StatusCode() != http.StatusCreated {
		utilfw.UnableToCreateResourceError(resp, response.String())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *permissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state permissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var permission PermissionAPIModel

	response, err := r.ProviderData.Client.R().
		SetPathParam("name", state.Name.ValueString()).
		SetResult(&permission).
		Get(PermissionEndpoint + "/{name}")

	// Treat HTTP 404 Not Found status as a signal to recreate resource
	// and return early
	if err != nil {
		if response.StatusCode() == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		utilfw.UnableToRefreshResourceError(resp, response.String())
		return
	}

	// Convert from the API data model to the Terraform data model
	// and refresh any attribute values.
	resp.Diagnostics.Append(state.fromAPIModel(ctx, &permission)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Read1", map[string]any{
		"state": state,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *permissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan permissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var permission PermissionAPIModel
	resp.Diagnostics.Append(plan.toAPIModel(ctx, &permission)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// permission can only be updated by resource type, not in its entirety!
	// so loop through every field and update each value
	for resourceType, resourceValue := range permission.Resources {
		_, err := r.ProviderData.Client.R().
			SetPathParams(map[string]string{
				"name":         plan.Name.ValueString(),
				"resourceType": resourceType,
			}).
			SetBody(resourceValue).
			Put(PermissionEndpoint + "/{name}/{resourceType}")
		if err != nil {
			utilfw.UnableToUpdateResourceError(resp, err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *permissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data permissionResourceModel

	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := r.ProviderData.Client.R().
		SetPathParam("name", data.Name.ValueString()).
		Delete(PermissionEndpoint + "/{name}")
	if err != nil {
		utilfw.UnableToDeleteResourceError(resp, err.Error())
		return
	}

	if response.StatusCode() != http.StatusNoContent {
		utilfw.UnableToDeleteResourceError(resp, response.String())
		return
	}

	// If the logic reaches here, it implicitly succeeded and will remove
	// the resource from state if there are no other errors.
}

func (r *permissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
