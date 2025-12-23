package challenge

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlexEreh/terraform-provider-ctfd/provider/utils"
)

var (
	BehaviorHidden     = types.StringValue("hidden")
	BehaviorAnonymized = types.StringValue("anonymized")

	FunctionLinear      = types.StringValue("linear")
	FunctionLogarithmic = types.StringValue("logarithmic")

	FlagCaseInsensitive = types.StringValue("case_insensitive")

	FlagTypeStatic       = types.StringValue("static")
	FlagTypeRegex        = types.StringValue("regex")
	FlagTypeProgrammable = types.StringValue("programmable")

	// File types/locations for CTFd Files API
	FileTypeChallenge = types.StringValue("challenge")

	FileLocationChallenge = types.StringValue("challenge")
)

type RequirementsSubresourceModel struct {
	Behavior      types.String   `tfsdk:"behavior"`
	Prerequisites []types.String `tfsdk:"prerequisites"`
}

type FlagSubresourceModel struct {
	Type types.String `tfsdk:"type"`
	Case types.String `tfsdk:"case"`
	Flag types.String `tfsdk:"flag"`
}

// FileSubresourceModel describes a single file attached to a challenge.
// "path" is a local filesystem path used only for upload (write-only, ForceNew
// in the schema); CTFd API does not let us read file content back, only
// metadata such as id/name/location.
type FileSubresourceModel struct {
	ID         types.Int64  `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Path       types.String `tfsdk:"path"`
	Type       types.String `tfsdk:"type"`
	Location   types.String `tfsdk:"location"`
	Challenge  types.Int64  `tfsdk:"challenge_id"`
	URL        types.String `tfsdk:"url"`
	AccessType types.String `tfsdk:"access_type"`
}

func GetAnon(str types.String) *bool {
	switch {
	case str.Equal(BehaviorHidden):
		return nil
	case str.Equal(BehaviorAnonymized):
		return utils.Ptr(true)
	}
	panic("invalid anonymization value: " + str.ValueString())
}

func FromAnon(b *bool) types.String {
	if b == nil {
		return BehaviorHidden
	}
	if *b {
		return BehaviorAnonymized
	}
	panic("invalid anonymization value, got boolean false")
}
