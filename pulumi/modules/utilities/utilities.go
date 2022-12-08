package utilities

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func IdOutputArrayToStringOutputArray(as []pulumi.IDOutput) pulumi.StringArray {
	a := make(pulumi.StringArray, len(as))
	for i, v := range as {
		a[i] = v.ToStringOutput()
	}
	return a
}
