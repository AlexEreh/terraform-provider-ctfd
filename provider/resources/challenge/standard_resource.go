package challenge

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ctfer-io/go-ctfd/api"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/AlexEreh/terraform-provider-ctfd/provider/utils"
	"github.com/AlexEreh/terraform-provider-ctfd/provider/validators"
)

var (
	_ resource.Resource                = (*challengeStandardResource)(nil)
	_ resource.ResourceWithConfigure   = (*challengeStandardResource)(nil)
	_ resource.ResourceWithImportState = (*challengeStandardResource)(nil)
)

func NewChallengeStandardResource() resource.Resource {
	return &challengeStandardResource{}
}

type challengeStandardResource struct {
	client *api.Client
}

// ChallengeStandardResourceModel is exported for ease of extending
// CTFd through a plugin. Under normal circumpstances, you should
// not use it.
type ChallengeStandardResourceModel struct {
	ID             types.String                  `tfsdk:"id"`
	Name           types.String                  `tfsdk:"name"`
	Category       types.String                  `tfsdk:"category"`
	Description    types.String                  `tfsdk:"description"`
	Attribution    types.String                  `tfsdk:"attribution"`
	ConnectionInfo types.String                  `tfsdk:"connection_info"`
	MaxAttempts    types.Int64                   `tfsdk:"max_attempts"`
	Value          types.Int64                   `tfsdk:"value"`
	Logic          types.String                  `tfsdk:"logic"`
	State          types.String                  `tfsdk:"state"`
	Next           types.Int64                   `tfsdk:"next"`
	Requirements   *RequirementsSubresourceModel `tfsdk:"requirements"`
	Flag           *FlagSubresourceModel         `tfsdk:"flag"`
	Tags           []types.String                `tfsdk:"tags"`
	Topics         []types.String                `tfsdk:"topics"`
	// Attached files (subresource) for the challenge.
	Files []FileSubresourceModel `tfsdk:"files"`
}

func (r *challengeStandardResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_challenge_standard"
}

func (r *challengeStandardResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "CTFd is built around the Challenge resource, which contains all the attributes to define a part of the Capture The Flag event.\n\nIt is the first historic implementation of its kind, with basic functionalities.",
		Attributes:          ChallengeStandardResourceAttributes,
	}
}

func (r *challengeStandardResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *github.com/ctfer-io/go-ctfd/api.Client, got: %T. Please open an issue at https://github.com/ctfer-io/terraform-provider-ctfd", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *challengeStandardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ChallengeStandardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create Challenge
	reqs := (*api.Requirements)(nil)
	if data.Requirements != nil {
		preqs := make([]int, 0, len(data.Requirements.Prerequisites))
		for _, preq := range data.Requirements.Prerequisites {
			id, _ := strconv.Atoi(preq.ValueString())
			preqs = append(preqs, id)
		}
		reqs = &api.Requirements{
			Anonymize:     GetAnon(data.Requirements.Behavior),
			Prerequisites: preqs,
		}
	}
	res, err := r.client.PostChallenges(&api.PostChallengesParams{
		Name:           data.Name.ValueString(),
		Category:       data.Category.ValueString(),
		Description:    data.Description.ValueString(),
		Attribution:    data.Attribution.ValueStringPointer(),
		ConnectionInfo: data.ConnectionInfo.ValueStringPointer(),
		MaxAttempts:    utils.ToInt(data.MaxAttempts),
		Value:          int(data.Value.ValueInt64()),
		Logic:          data.Logic.ValueString(),
		State:          data.State.ValueString(),
		Type:           "standard",
		NextID:         utils.ToInt(data.Next),
		Requirements:   reqs,
	}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to create challenge, got error: %s", err),
		)
		return
	}

	tflog.Trace(ctx, "created a challenge")

	// Save computed attributes in state
	data.ID = types.StringValue(strconv.Itoa(res.ID))

	// Create flag, if requested
	if data.Flag != nil {
		flagType := data.Flag.Type
		if flagType.IsNull() || flagType.IsUnknown() {
			flagType = FlagTypeStatic
		}
		if _, err := r.client.PostFlags(&api.PostFlagsParams{
			Challenge: res.ID,
			Content:   data.Flag.Flag.ValueString(),
			Data:      "",
			Type:      flagType.ValueString(),
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create flag, got error: %s", err))
			return
		}
	}

	// Create tags
	challTags := make([]types.String, 0, len(data.Tags))
	for _, tag := range data.Tags {
		_, err := r.client.PostTags(&api.PostTagsParams{
			Challenge: utils.Atoi(data.ID.ValueString()),
			Value:     tag.ValueString(),
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to create tags, got error: %s", err),
			)
			return
		}
		challTags = append(challTags, tag)
	}
	if data.Tags != nil {
		data.Tags = challTags
	}

	// Create topics
	challTopics := make([]types.String, 0, len(data.Topics))
	for _, topic := range data.Topics {
		_, err := r.client.PostTopics(&api.PostTopicsParams{
			Challenge: utils.Atoi(data.ID.ValueString()),
			Type:      "challenge",
			Value:     topic.ValueString(),
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to create topic, got error: %s", err),
			)
			return
		}
		challTopics = append(challTopics, topic)
	}
	if data.Topics != nil {
		data.Topics = challTopics
	}

	// Create files
	if len(data.Files) > 0 {
		uploadedFiles, fileDiags := CreateChallengeFiles(ctx, r.client, res.ID, data.Files)
		resp.Diagnostics.Append(fileDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Files = uploadedFiles
	}

	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *challengeStandardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ChallengeStandardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Read(ctx, r.client, resp.Diagnostics)

	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *challengeStandardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ChallengeStandardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var dataState ChallengeStandardResourceModel
	req.State.Get(ctx, &dataState)

	// Patch direct attributes
	reqs := (*api.Requirements)(nil)
	if data.Requirements != nil {
		preqs := make([]int, 0, len(data.Requirements.Prerequisites))
		for _, preq := range data.Requirements.Prerequisites {
			id, _ := strconv.Atoi(preq.ValueString())
			preqs = append(preqs, id)
		}
		reqs = &api.Requirements{
			Anonymize:     GetAnon(data.Requirements.Behavior),
			Prerequisites: preqs,
		}
	}
	_, err := r.client.PatchChallenge(utils.Atoi(data.ID.ValueString()), &api.PatchChallengeParams{
		Name:           data.Name.ValueString(),
		Category:       data.Category.ValueString(),
		Description:    data.Description.ValueString(),
		Attribution:    data.Attribution.ValueStringPointer(),
		ConnectionInfo: data.ConnectionInfo.ValueStringPointer(),
		MaxAttempts:    utils.ToInt(data.MaxAttempts),
		Value:          utils.ToInt(data.Value),
		Logic:          data.Logic.ValueStringPointer(),
		State:          data.State.ValueString(),
		NextID:         utils.ToInt(data.Next),
		Requirements:   reqs,
	}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to update challenge, got error: %s", err),
		)
		return
	}

	// Update its tags (drop them all, create new ones)
	challTags, err := r.client.GetChallengeTags(utils.Atoi(data.ID.ValueString()), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to get all tags of challenge %s, got error: %s", data.ID.ValueString(), err),
		)
		return
	}
	for _, tag := range challTags {
		if err := r.client.DeleteTag(strconv.Itoa(tag.ID), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to delete tag %d of challenge %s, got error: %s", tag.ID, data.ID.ValueString(), err),
			)
			return
		}
	}
	tags := make([]types.String, 0, len(data.Tags))
	for _, tag := range data.Tags {
		_, err := r.client.PostTags(&api.PostTagsParams{
			Challenge: utils.Atoi(data.ID.ValueString()),
			Value:     tag.ValueString(),
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to create tag of challenge %s, got error: %s", data.ID.ValueString(), err),
			)
			return
		}
		tags = append(tags, tag)
	}
	if data.Tags != nil {
		data.Tags = tags
	}

	// Update its topics (drop them all, create new ones)
	challTopics, err := r.client.GetChallengeTopics(utils.Atoi(data.ID.ValueString()), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to get all topics of challenge %s, got error: %s", data.ID.ValueString(), err),
		)
		return
	}
	for _, topic := range challTopics {
		if err := r.client.DeleteTopic(&api.DeleteTopicArgs{
			ID:   strconv.Itoa(topic.ID),
			Type: "challenge",
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to delete topic %d of challenge %s, got error: %s", topic.ID, data.ID.ValueString(), err),
			)
			return
		}
	}
	topics := make([]types.String, 0, len(data.Topics))
	for _, topic := range data.Topics {
		_, err := r.client.PostTopics(&api.PostTopicsParams{
			Challenge: utils.Atoi(data.ID.ValueString()),
			Type:      "challenge",
			Value:     topic.ValueString(),
		}, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to create topic of challenge %s, got error: %s", data.ID.ValueString(), err),
			)
			return
		}
		topics = append(topics, topic)
	}
	if data.Topics != nil {
		data.Topics = topics
	}

	// Update files
	syncedFiles, fileDiags := SyncChallengeFilesOnUpdate(ctx, r.client, utils.Atoi(data.ID.ValueString()), dataState.Files, data.Files)
	resp.Diagnostics.Append(fileDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Files = syncedFiles

	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *challengeStandardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ChallengeStandardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Best-effort flag cleanup; CTFd will also remove nested data when deleting the challenge.
	if err := r.client.DeleteFlag(strconv.Itoa(utils.Atoi(data.ID.ValueString())), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete flag for challenge, got error: %s", err))
	}

	if err := r.client.DeleteChallenge(utils.Atoi(data.ID.ValueString()), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil))); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete challenge, got error: %s", err))
		return
	}

	// ... don't need to delete nested objects, this is handled by CTFd
}

func (r *challengeStandardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)

	// Automatically call r.Read
}

//
// Starting from this are helper or types-specific code related to the ctfd_challenge_standard resource
//

func (chall *ChallengeStandardResourceModel) Read(ctx context.Context, client *api.Client, diags diag.Diagnostics) {
	res, err := client.GetChallenge(utils.Atoi(chall.ID.ValueString()), api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to read challenge %s, got error: %s", chall.ID.ValueString(), err))
		return
	}
	chall.Name = types.StringValue(res.Name)
	chall.Category = types.StringValue(res.Category)
	chall.Description = types.StringValue(res.Description)
	chall.Attribution = types.StringPointerValue(res.Attribution)
	chall.ConnectionInfo = utils.ToTFString(res.ConnectionInfo)
	chall.MaxAttempts = utils.ToTFInt64(res.MaxAttempts)
	chall.Value = types.Int64Value(int64(res.Value))
	chall.Logic = types.StringValue(res.Logic)
	chall.State = types.StringValue(res.State)
	chall.Next = utils.ToTFInt64(res.NextID)

	id := utils.Atoi(chall.ID.ValueString())

	// Get subresources
	// => Requirements
	resReqs, err := client.GetChallengeRequirements(id, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		diags.AddError(
			"Client Error",
			fmt.Sprintf("Unable to read challenge %d requirements, got error: %s", id, err),
		)
		return
	}
	reqs := (*RequirementsSubresourceModel)(nil)
	if resReqs != nil {
		challPreqs := make([]types.String, 0, len(resReqs.Prerequisites))
		for _, req := range resReqs.Prerequisites {
			challPreqs = append(challPreqs, types.StringValue(strconv.Itoa(req)))
		}
		reqs = &RequirementsSubresourceModel{
			Behavior:      FromAnon(resReqs.Anonymize),
			Prerequisites: challPreqs,
		}
	}
	chall.Requirements = reqs

	// => Tags
	resTags, err := client.GetChallengeTags(id, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		diags.AddError(
			"Client Error",
			fmt.Sprintf("Unable to read challenge %d tags, got error: %s", id, err),
		)
		return
	}
	chall.Tags = make([]basetypes.StringValue, 0, len(resTags))
	for _, tag := range resTags {
		chall.Tags = append(chall.Tags, types.StringValue(tag.Value))
	}

	// => Topics
	resTopics, err := client.GetChallengeTopics(id, api.WithContext(ctx), api.WithTransport(otelhttp.NewTransport(nil)))
	if err != nil {
		diags.AddError(
			"Client Error",
			fmt.Sprintf("Unable to read challenge %d topics, got error: %s", id, err),
		)
		return
	}
	chall.Topics = make([]basetypes.StringValue, 0, len(resTopics))
	for _, topic := range resTopics {
		chall.Topics = append(chall.Topics, types.StringValue(topic.Value))
	}

	// => Files
	filesList, fileDiags := ReadChallengeFiles(ctx, client, id)
	diags.Append(fileDiags...)
	if diags.HasError() {
		return
	}
	chall.Files = filesList

	// => Flag (single, if exists)
	// NOTE: go-ctfd/api currently doesn't expose GetChallengeFlags helper,
	// so this part will be implemented once the client supports reading flags
	// per challenge. For now we keep chall.Flag as-is (from prior state/plan).
}

var (
	// ChallengeStandardResourceAttributes is exported for ease of extending
	// CTFd through a plugin. Under normal circumpstances, you should
	// not use it.
	ChallengeStandardResourceAttributes = map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the challenge.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"name": schema.StringAttribute{
			MarkdownDescription: "Name of the challenge, displayed as it.",
			Required:            true,
		},
		"category": schema.StringAttribute{
			MarkdownDescription: "Category of the challenge that CTFd groups by on the web UI.",
			Required:            true,
		},
		"description": schema.StringAttribute{
			MarkdownDescription: "Description of the challenge, consider using multiline descriptions for better style.",
			Required:            true,
		},
		"attribution": schema.StringAttribute{
			MarkdownDescription: "Attribution to the creator(s) of the challenge.",
			Optional:            true,
		},
		"connection_info": schema.StringAttribute{
			MarkdownDescription: "Connection Information to connect to the challenge instance, useful for pwn, web and infrastructure pentests.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString(""),
		},
		"max_attempts": schema.Int64Attribute{
			MarkdownDescription: "Maximum amount of attempts before being unable to flag the challenge.",
			Optional:            true,
			Computed:            true,
			Default:             int64default.StaticInt64(0),
		},
		"value": schema.Int64Attribute{
			MarkdownDescription: "The value (points) of the challenge once solved.",
			Required:            true,
		},
		"logic": schema.StringAttribute{
			MarkdownDescription: "The flag validation logic.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString("any"),
			Validators: []validator.String{
				validators.NewStringEnumValidator([]basetypes.StringValue{
					types.StringValue("any"),
					types.StringValue("all"),
					types.StringValue("team"),
				}),
			},
		},
		"state": schema.StringAttribute{
			MarkdownDescription: "State of the challenge, either hidden or visible.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString("hidden"),
			Validators: []validator.String{
				validators.NewStringEnumValidator([]basetypes.StringValue{
					types.StringValue("hidden"),
					types.StringValue("visible"),
				}),
			},
		},
		"next": schema.Int64Attribute{
			MarkdownDescription: "Suggestion for the end-user as next challenge to work on.",
			Optional:            true,
		},
		"requirements": schema.SingleNestedAttribute{
			MarkdownDescription: "List of required challenges that needs to get flagged before this one being accessible. Useful for skill-trees-like strategy CTF.",
			Optional:            true,
			Attributes: map[string]schema.Attribute{
				"behavior": schema.StringAttribute{
					MarkdownDescription: "Behavior if not unlocked, either hidden or anonymized.",
					Optional:            true,
					Computed:            true,
					Default:             stringdefault.StaticString("hidden"),
					Validators: []validator.String{
						validators.NewStringEnumValidator([]basetypes.StringValue{
							BehaviorHidden,
							BehaviorAnonymized,
						}),
					},
				},
				"prerequisites": schema.ListAttribute{
					MarkdownDescription: "List of the challenges ID.",
					Optional:            true,
					ElementType:         types.StringType,
				},
			},
		},
		"flag": schema.SingleNestedAttribute{
			MarkdownDescription: "Challenge flag definition. Only a single flag per challenge is managed by this provider.",
			Optional:            true,
			Attributes: map[string]schema.Attribute{
				"type": schema.StringAttribute{
					MarkdownDescription: "Type of the flag (static, regex, programmable).",
					Optional:            true,
					Computed:            true,
					Default:             stringdefault.StaticString(FlagTypeStatic.ValueString()),
					Validators: []validator.String{
						validators.NewStringEnumValidator([]basetypes.StringValue{
							FlagTypeStatic,
							FlagTypeRegex,
							FlagTypeProgrammable,
						}),
					},
				},
				"case": schema.StringAttribute{
					MarkdownDescription: "Case-sensitivity behavior of the flag.",
					Optional:            true,
					Computed:            true,
					Default:             stringdefault.StaticString(FlagCaseInsensitive.ValueString()),
				},
				"flag": schema.StringAttribute{
					MarkdownDescription: "Flag content.",
					Required:            true,
				},
			},
		},
		"tags": schema.ListAttribute{
			MarkdownDescription: "List of challenge tags that will be displayed to the end-user. You could use them to give some quick insights of what a challenge involves.",
			ElementType:         types.StringType,
			Optional:            true,
			Computed:            true,
			Default:             listdefault.StaticValue(basetypes.NewListValueMust(types.StringType, []attr.Value{})),
		},
		"topics": schema.ListAttribute{
			MarkdownDescription: "List of challenge topics that are displayed to the administrators for maintenance and planification.",
			ElementType:         types.StringType,
			Optional:            true,
			Computed:            true,
			Default:             listdefault.StaticValue(basetypes.NewListValueMust(types.StringType, []attr.Value{})),
		},
		"files": schema.ListNestedAttribute{
			MarkdownDescription: "List of files (attachments) associated with this challenge.",
			Optional:            true,
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"id": schema.Int64Attribute{
						MarkdownDescription: "Identifier of the file in CTFd.",
						Computed:            true,
					},
					"name": schema.StringAttribute{
						MarkdownDescription: "Logical name of the file in CTFd.",
						Required:            true,
					},
					"path": schema.StringAttribute{
						MarkdownDescription: "Local filesystem path to upload as this file (write-only, ForceNew).",
						Optional:            true,
						Sensitive:           true,
					},
					"type": schema.StringAttribute{
						MarkdownDescription: "Type of the file entry in CTFd (e.g., challenge).",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString(FileTypeChallenge.ValueString()),
					},
					"location": schema.StringAttribute{
						MarkdownDescription: "Location namespace of the file in CTFd (typically 'challenge').",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString(FileLocationChallenge.ValueString()),
					},
					"challenge_id": schema.Int64Attribute{
						MarkdownDescription: "Challenge identifier this file is attached to.",
						Computed:            true,
					},
					"url": schema.StringAttribute{
						MarkdownDescription: "Public or internal URL to the file as returned by CTFd.",
						Computed:            true,
					},
					"access_type": schema.StringAttribute{
						MarkdownDescription: "Access control type of the file (if exposed by the API).",
						Computed:            true,
					},
				},
			},
		},
	}
)
