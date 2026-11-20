// Package tushare 是 tushare pro 数据接口的 Go SDK。
//
// 设计目标是与 Python 版 tushare 保持一致的调用方式，同时符合 Go 的使用习惯：
//
//   - 底层 Query 对应 pro.query(api_name, **kwargs)，可调用任意接口；
//   - 268 个接口都有强类型的便捷方法（如 Daily、Income），参数结构体带字段说明；
//   - 结果集 Table 对应 pandas.DataFrame，支持 Scan 到结构体、导出 CSV、按列取值与迭代；
//   - GetBar 对应 tushare 1.4.29 的 ts.pro_bar，在客户端完成复权、均线与因子合并；
//   - 内置重试、限流、结构化日志与类型化错误，零第三方依赖。
//
// # 快速开始
//
//	cli, err := tushare.NewClient("your-token") // 或设置环境变量 TUSHARE_TOKEN
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// 强类型接口：参数有补全、字段有注释
//	tbl, err := cli.Daily(context.Background(), &tushare.DailyParams{
//		TsCode:    "000001.SZ",
//		StartDate: "20240101",
//		EndDate:   "20240131",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// 方式一：按列名遍历
//	for row := range tbl.Rows() {
//		fmt.Println(row.Str("trade_date"), row.Float("close"))
//	}
//
//	// 方式二：扫描到自己定义的结构体
//	type Bar struct {
//		TradeDate string  `tushare:"trade_date"`
//		Close     float64 `tushare:"close"`
//	}
//	var bars []Bar
//	if err := tbl.Scan(&bars); err != nil {
//		log.Fatal(err)
//	}
//
// # 复权行情
//
//	bars, err := cli.GetBar(ctx, &tushare.BarQuery{
//		TsCode:    "000001.SZ",
//		StartDate: "20240101",
//		Adj:       tushare.AdjQFQ,
//		MA:        []int{5, 20},
//	})
//
// # 错误处理
//
// 所有错误都是 *APIError，可按归类分支处理：
//
//	if tushare.IsRateLimited(err) {
//		// 等待后重试
//	} else if tushare.IsPermission(err) {
//		// 积分或权限不足
//	}
//
// # 接口清单
//
// 全量接口元数据（入参、出参、分类、文档链接）保存在 spec/endpoints.json，
// 由 cmd/tusharegen 生成本包的 api_*.gen.go，可用 go generate ./... 重新生成。
package tushare
