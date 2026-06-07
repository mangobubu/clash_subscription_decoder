package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	fmt.Println("==================================================")
	fmt.Println("       Clash Subscription Decoder 全栈一键自动化构建系统")
	fmt.Println("==================================================")

	// 1. 确定项目目录结构
	wd, err := os.Getwd()
	if err != nil {
		fmt.Printf("❌ 获取当前工作目录失败: %v\n", err)
		os.Exit(1)
	}

	frontendDir := filepath.Join(wd, "frontend")
	backendDir := filepath.Join(wd, "backend")
	releaseDir := filepath.Join(wd, "release")

	// 验证前后端文件夹是否存在
	if !dirExists(frontendDir) || !dirExists(backendDir) {
		fmt.Println("❌ 错误: 未能在当前目录下找到 'frontend' 或 'backend' 文件夹！")
		fmt.Println("💡 请确保您在 clash-subscription-decoder 项目根目录下运行此脚本。")
		os.Exit(1)
	}

	// 自动创建 release 产物目录
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		fmt.Printf("❌ 创建 release 产物目录失败: %v\n", err)
		os.Exit(1)
	}

	// 2. 探测包管理器 (优先使用 pnpm，若无则自动回退到 npm)
	fmt.Println("🔍 正在探测系统中的包管理器...")
	pm := "pnpm"
	if _, err := exec.LookPath("pnpm"); err != nil {
		fmt.Println("⚠️  未找到 pnpm，正在自动降级并回退至使用 npm...")
		pm = "npm"
		if _, err := exec.LookPath("npm"); err != nil {
			fmt.Println("❌ 错误: 本地未安装 npm 或 pnpm 包管理器！请先安装 Node.js 与 npm。")
			os.Exit(1)
		}
	}
	fmt.Printf("✅ 已选定包管理器: %s\n\n", pm)

	// 3. 执行前端依赖安装
	fmt.Printf("📦 正在前端目录安装依赖 [%s install]...\n", pm)
	if err := runCommand(frontendDir, pm, "install"); err != nil {
		fmt.Printf("❌ 前端依赖安装失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 前端依赖安装成功！\n\n")

	// 4. 执行前端生产环境打包
	fmt.Printf("🚀 正在编译前端静态资源 [%s run build]...\n", pm)
	if err := runCommand(frontendDir, pm, "run", "build"); err != nil {
		fmt.Printf("❌ 前端打包失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 前端打包并直出至 backend/dist 成功！\n\n")

	// 5. 执行后端 Go 二进制编译并输出到 release 目录
	binaryName := "Clash Subscription Decoder"
	if runtime.GOOS == "windows" {
		binaryName = "Clash Subscription Decoder.exe"
	}
	binaryPath := filepath.Join(releaseDir, binaryName)
	fmt.Printf("🔨 正在编译 Go 后端应用，并汇聚至 release/%s...\n", binaryName)
	if err := runCommand(backendDir, "go", "build", "-o", filepath.Join("../release", binaryName)); err != nil {
		fmt.Printf("❌ 后端编译失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 后端应用编译成功！\n\n")

	// 6. 自动拷贝并汇聚配置文件
	fmt.Println("📝 正在进行运行配置的自动汇聚与保护...")
	exampleSrc := filepath.Join(backendDir, "config.example.toml")
	exampleDst := filepath.Join(releaseDir, "config.example.toml")

	// 清理 release 目录下冗余的 config.example.toml，保持部署文件夹的极致清爽与极简
	if _, err := os.Stat(exampleDst); err == nil {
		_ = os.Remove(exampleDst)
	}

	// 自动创建初始的 config.toml
	configDst := filepath.Join(releaseDir, "config.toml")
	if _, err := os.Stat(configDst); os.IsNotExist(err) {
		if err := copyFile(exampleSrc, configDst); err != nil {
			fmt.Printf("⚠️  自动生成初始 config.toml 失败: %v\n", err)
		} else {
			fmt.Println("  - 🌟 已自动为您生成全新的初始配置文件 [config.toml]！")
		}
	} else {
		fmt.Println("  - ℹ️  检测到已存在 [config.toml]，已安全跳过覆盖，全力保障您本地数据库配置的完整性")
	}

	fmt.Println("\n==================================================")
	fmt.Println("🎉 全栈独立产物目录构建成功！")
	fmt.Printf("👉 产物位置: %s\n", binaryPath)
	fmt.Printf("👉 部署目录: %s\n", releaseDir)
	fmt.Println("💡 提示: 您仅需整体拷贝该 release 目录即可前往任何服务器一键部署！")
	fmt.Println("==================================================")
}

// dirExists 检查文件夹是否存在
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

// copyFile 复制文件辅助函数
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// runCommand 跨平台执行命令的通用封装
func runCommand(dir string, name string, args ...string) error {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		// 在 Windows 环境下，npm 和 pnpm 是批处理脚本 (.cmd)，必须通过 cmd.exe /c 来调起。
		// 而 go 本身是标准的 .exe 可执行二进制，可以直接运行。
		if name == "npm" || name == "pnpm" {
			fullArgs := append([]string{"/c", name}, args...)
			cmd = exec.Command("cmd", fullArgs...)
		} else {
			cmd = exec.Command(name, args...)
		}
	} else {
		cmd = exec.Command(name, args...)
	}

	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
