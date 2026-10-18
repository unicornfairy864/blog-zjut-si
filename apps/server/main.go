// Command blogd 是 zjut.si 博客的服务端进程。
//
// 只做两件事：初始化（initialize.Init）与运行（initialize.Run）。
// 两条 listener：:8640 公开边缘、:8641 管理边缘（C3/C4/C5）。
package main

import "github.com/unicornfairy864/blog-zjut-si/apps/server/initialize"

func main() {
	initialize.Init()
	initialize.Run()
}
