# tushare-go-sdk

[Tushare Pro](https://tushare.pro) 的 Go SDK。调用方式和 Python `tushare` 一致：`Query` 对应 `pro.query`，`Daily` 等方法对应 `pro.daily`，`GetBar` 对应 `ts.pro_bar`。

需要 Go 1.23+。Token 见 [tushare.pro/user/token](https://tushare.pro/user/token)。

```bash
go get github.com/dekeky/tushare-go-sdk
```

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/dekeky/tushare-go-sdk"
)

func main() {
	cli, err := tushare.NewClient("your token")
	if err != nil {
		log.Fatal(err)
	}

	tbl, err := cli.Daily(context.Background(), &tushare.DailyParams{
		TsCode:    "000001.SZ",
		StartDate: "20240101",
		EndDate:   "20240131",
	})
	if err != nil {
		log.Fatal(err)
	}

	for row := range tbl.Rows() {
		fmt.Println(row.Str("trade_date"), row.Float("close"))
	}
}
```
