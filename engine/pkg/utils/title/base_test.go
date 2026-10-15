/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package title
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2023/7/3 0003 18:13
// 最后更新:  yr  2023/7/3 0003 18:13
package title

import (
	"fmt"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/translate"
	"testing"
)

// 这是3D-ASCII风格的title
var titleWithColor = `
Supported by:
[1;31m _______   [0m[1;33m ______________   [0m[1;32m ________   [0m[1;34m _______    [0m[1;35m _______     [0m   
[1;31m|\  ____\  [0m[1;33m|\   _    _ _  \  [0m[1;32m|\   __  \  [0m[1;34m|\  ____\   [0m[1;35m|\   __  \   [0m  
[1;31m\ \  \____ [0m[1;33m\ \  \ \\\\\ \  \ [0m[1;32m\ \  \_\  \ [0m[1;34m\ \  \____  [0m[1;35m\ \  \_\  \  [0m 
[1;31m \ \  \___\ [0m[1;33m\ \  \ \\\\\ \  \[0m[1;32m \ \   __  \[0m[1;34m \ \  \___\ [0m[1;35m \ \   _  _\ [0m 
[1;31m  \ \  \____[0m[1;33m \ \  \     \ \  \[0m[1;32m \ \  \_\  \[0m[1;34m \ \  \____[0m[1;35m  \ \  \\  \ [0m 
[1;31m   \ \_______\[0m[1;33m\ \__\     \ \__\[0m[1;32m \ \_______\[0m[1;34m \ \_______\[0m[1;35m \ \__\\__\[0m  
[1;31m    \/_______/[0m[1;33m \/__/      \/__/[0m[1;32m  \/_______/[0m[1;34m  \/_______/[0m[1;35m  \/__//__/[0m  

`

// 这是ANSI Shadow风格
var title1Base = `
Supported by:
				███████╗███╗   ███╗██████╗ ███████╗██████╗ 
				██╔════╝████╗ ████║██╔══██╗██╔════╝██╔══██╗
				█████╗  ██╔████╔██║██████╔╝█████╗  ██████╔╝
				██╔══╝  ██║╚██╔╝██║██╔══██╗██╔══╝  ██╔══██╗
				███████╗██║ ╚═╝ ██║██████╔╝███████╗██║  ██║
				╚══════╝╚═╝     ╚═╝╚═════╝ ╚══════╝╚═╝  ╚═╝
`

// 这是Bloody风格
var title2 = `

				▓█████  ███▄ ▄███▓ ▄▄▄▄   ▓█████  ██▀███  
				▓█   ▀ ▓██▒▀█▀ ██▒▓█████▄ ▓█   ▀ ▓██ ▒ ██▒
				▒███   ▓██    ▓██░▒██▒ ▄██▒███   ▓██ ░▄█ ▒
				▒▓█  ▄ ▒██    ▒██ ▒██░█▀  ▒▓█  ▄ ▒██▀▀█▄  
				░▒████▒▒██▒   ░██▒░▓█  ▀█▓░▒████▒░██▓ ▒██▒
				░░ ▒░ ░░ ▒░   ░  ░░▒▓███▀▒░░ ▒░ ░░ ▒▓ ░▒▓░
				 ░ ░  ░░  ░      ░▒░▒   ░  ░ ░  ░  ░▒ ░ ▒░
				   ░   ░      ░    ░    ░    ░     ░░   ░ 
				   ░  ░       ░    ░         ░  ░   ░     
										░                 
				 %s • %s: %s


`

func EchoTitleByType(tp int, version string) {
	switch tp {
	case 0:
		fmt.Print(fmt.Sprintf(title2, translate.Translate("Powered by Ember Framework"), translate.Translate("Version"), version))
	case 1:
		fmt.Print(fmt.Sprintf(title2, translate.Translate("Powered by Ember Framework"), translate.Translate("Version"), version))
	}
}

func TestEchoTitle(t *testing.T) {
	EchoTitleByType(0, "2.0")
}
