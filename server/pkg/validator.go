package pkg

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ValidateStruct 校验结构体，返回第一条校验错误信息。
func ValidateStruct(s any) error {
	if err := validate.Struct(s); err != nil {
		if ves, ok := err.(validator.ValidationErrors); ok && len(ves) > 0 {
			return ErrValidation(fmt.Sprintf("%s 校验失败", ves[0].Field()))
		}
		return ErrValidation(err.Error())
	}
	return nil
}

// ValidateEnum 校验值在合法枚举集合内。
func ValidateEnum(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return ErrValidation(fmt.Sprintf("%s 必须是以下值之一: %s", field, strings.Join(allowed, ", ")))
}
