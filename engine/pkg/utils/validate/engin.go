package validate

import (
	"bytes"
	"errors"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhTrans "github.com/go-playground/validator/v10/translations/zh"
	"strings"
)

var (
	zhT        = zh.New() //中文翻译器
	enT        = en.New() //英文翻译器
	translator = ut.New(enT, zhT, enT)
	Validator  = validator.New()
)

const (
	ZH = `zh` // 中文
	EN = `en` // 英文
)

func init() {
	Validator.SetTagName(`binding`)
	tran, _ := translator.GetTranslator(ZH)
	_ = zhTrans.RegisterDefaultTranslations(Validator, tran)
}

// TransError 翻译错误信息
func TransError(err error, locale ...string) error {
	errs, ok := err.(validator.ValidationErrors)
	if !ok {
		return err
	}
	lang := append(locale, ZH)[0]
	var tran ut.Translator
	switch lang {
	case ZH:
		tran, _ = translator.GetTranslator(ZH)
	case EN:
		tran, _ = translator.GetTranslator(EN)
	default:
		tran, _ = translator.GetTranslator(ZH)
	}
	var limit = `; `
	buff := bytes.NewBuffer(nil)
	for _, s2 := range errs.Translate(tran) {
		buff.WriteString(s2 + limit)
	}
	return errors.New(strings.TrimSuffix(buff.String(), limit))
}

// Struct 验证结构体
func Struct(s interface{}) error {
	return Validator.Struct(s)
}
