package validate

import (
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	"github.com/robfig/cron/v3"
	"regexp"
)

// Custom 自定义验证器
type Custom struct {
	Name    string         // 验证标签名
	Func    validator.Func // 验证方法
	Chinese string         // 中文信息
	English string         // 英文信息
}

var (
	phoneReg    = regexp.MustCompile(`^(86)?1[2-9]\d{9}$`)
	sm3Reg      = regexp.MustCompile(`^[a-f\d]{64}$`)
	usernameReg = regexp.MustCompile(`^[\w_@.]{4,24}$`)
	pwdReg      = regexp.MustCompile(`^[\w_!@#$%^&*]{6,20}$`)
	locales     = []string{ZH, EN}
)

func init() {
	customs := []*Custom{
		{
			Name: `phone`,
			Func: func(fl validator.FieldLevel) bool {
				return phoneReg.MatchString(fl.Field().String())
			},
			Chinese: `{0}必须是手机号`,
			English: `Field validation for '{0}' failed on the 'phone' tag`,
		}, {
			Name:    `hostname_port`,
			Func:    nil,
			Chinese: `{0}必须是域名加端口`,
			English: `Field validation for '{0}' failed on the 'hostname_port' tag`,
		}, {
			Name: `sm3`,
			Func: func(fl validator.FieldLevel) bool {
				return sm3Reg.MatchString(fl.Field().String())
			},
			Chinese: `{0}必须是SM3校验码`,
			English: `Field validation for '{0}' failed on the 'sm3' tag`,
		}, {
			Name: `username`,
			Func: func(fl validator.FieldLevel) bool {
				return usernameReg.MatchString(fl.Field().String())
			},
			Chinese: `{0}必须是4-24个有效字符`,
			English: `Field validation for '{0}' failed on the 'username' tag`,
		}, {
			Name: `password`,
			Func: func(fl validator.FieldLevel) bool {
				return pwdReg.MatchString(fl.Field().String())
			},
			Chinese: `{0}必须是4-24个有效字符`,
			English: `Field validation for '{0}' failed on the 'password' tag`,
		}, {
			Name: `cron`,
			Func: func(fl validator.FieldLevel) bool {
				_, err := cron.ParseStandard(fl.Field().String())
				return err == nil
			},
			Chinese: "{0}必须是有效定时规则(* * * * *)",
			English: "Field validation for '{0}' failed on the 'cron' tag",
		},
	}
	for _, custom := range customs {
		RegisterCustom(custom)
	}
}

func RegisterCustom(custom *Custom) {
	if custom.Func != nil {
		_ = Validator.RegisterValidation(custom.Name, custom.Func)
	}
	for _, locale := range locales {
		tran, _ := translator.GetTranslator(locale)
		_ = Validator.RegisterTranslation(
			custom.Name,
			tran,
			func(ut ut.Translator) error {
				switch ut.Locale() {
				case ZH:
					return ut.Add(custom.Name, custom.Chinese, true)
				case EN:
					return ut.Add(custom.Name, custom.English, true)
				}
				return nil
			},
			func(ut ut.Translator, fe validator.FieldError) string {
				t, _ := ut.T(custom.Name, fe.Field())
				return t
			},
		)
	}
}
