package runner

import (
	"fmt"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/wgpsec/ENScan/common"
	"github.com/wgpsec/ENScan/common/gologger"
	"github.com/wgpsec/ENScan/common/utils"
	_interface "github.com/wgpsec/ENScan/interface"
	"regexp"
)

func AdvanceFilter(job _interface.ENScan) string {
	enList, err := job.AdvanceFilter(job.GetEnsD().Name)
	enMap := job.GetENMap()["enterprise_info"]
	if err != nil {
		gologger.Error().Msg(err.Error())
	} else {
		gologger.Info().Msgf("关键词：“%s” 查询到 %d 个结果，默认选择第一个 \n", job.GetEnsD().Name, len(enList))
		//展示结果
		utils.TBS(append(enMap.KeyWord[:3], "PID"), append(enMap.Field[:3], enMap.Field[10]), "企业信息", enList)
		pid := enList[0].Get(enMap.Field[10]).String()
		gologger.Debug().Str("PID", pid).Msgf("搜索")
		return pid
	}
	return ""
}

// getInfoById 根据查询的ID查询公司信息，主要为判定投资类
func getInfoById(pid string, searchList []string, job _interface.ENScan) (enInfo map[string][]gjson.Result) {
	if pid == "" {
		gologger.Error().Msgf("获取PID为空！")
		return map[string][]gjson.Result{}
	}
	enMap := job.GetENMap()
	options := job.GetEnsD().Op
	// 基本信息获取
	enInfo = getCompanyInfoById(pid, "", searchList, job)
	enName := enInfo["enterprise_info"][0].Get(enMap["enterprise_info"].Field[0]).String()
	var ds []string
	for _, s := range searchList {
		if utils.IsInList(s, common.DeepSearch) {
			// 跳过分支机构搜索
			if s == "branch" && !options.IsSearchBranch {
				continue
			}
			ds = append(ds, s)
		}
	}
	if len(ds) > 0 {
		gologger.Info().Msgf("深度搜索列表：%v", ds)
	}
	var etNameFilter *regexp.Regexp
	if options.BranchFilter != "" {
		etNameFilter = regexp.MustCompile(options.BranchFilter)
	}
	for _, sk := range ds {
		enSk := enMap[sk].Field
		pidName := enSk[len(enSk)-2]
		etNameJ := enSk[0]
		scaleName := enSk[3]
		association := enMap[sk].Name
		if len(enInfo[sk]) == 0 {
			gologger.Info().Str("type", sk).Msgf("【x】%s 数量为空，跳过搜索\n", association)
			continue
		}

		if sk == "invest" {
			iEnData := make([][]gjson.Result, options.Deep)
			iEnData = append(iEnData, make([]gjson.Result, 0))
			// 投资信息赋值
			iEnData[0] = enInfo[sk]
			for i := 0; i < options.Deep; i++ {
				if len(iEnData[i]) <= 0 {
					break
				}
				var nextInK []gjson.Result
				for _, r := range iEnData[i] {
					tPid := r.Get(pidName).String()
					tName := r.Get(etNameJ).String()
					if etNameFilter != nil && etNameFilter.MatchString(tName) {
						gologger.Info().Msgf("根据过滤器跳过 [%s]", tName)
						continue
					}
					gologger.Debug().Str("PID", tPid).Str("Name", tName).Str("PID NAME", pidName).Msgf("查询PID")
					// 计算投资比例判断是否符合
					investNum := utils.FormatInvest(r.Get(scaleName).String())
					if investNum < options.InvestNum {
						continue
					}
					association = fmt.Sprintf("%s ⌈%d⌋级投资⌈%.2f%%⌋-%s", tName, i+1, investNum, enName)
					gologger.Info().Msgf("%s", association)
					dEnData := getCompanyInfoById(tPid, association, searchList, job)
					// 保存当前数据
					for dk, dr := range dEnData {
						enInfo[dk] = append(enInfo[dk], dr...)
					}
					// 存下一层需要跑的信息
					nextInK = append(nextInK, dEnData[sk]...)
				}
				iEnData[i+1] = nextInK
			}

		} else {
			association = fmt.Sprintf("%s %s", enMap[sk].Name, enName)
			gologger.Info().Msgf("%s", association)
			// 增加数据，该类型下的全部企业数据
			enLen := len(enInfo[sk])
			for i, r := range enInfo[sk] {
				gologger.Info().Msgf("[%d/%d]", i+1, enLen)
				tPid := r.Get(pidName).String()
				tName := r.Get(etNameJ).String()
				if etNameFilter != nil && etNameFilter.MatchString(tName) {
					gologger.Info().Msgf("根据过滤器跳过 [%s]", tName)
					continue
				}
				dEnData := getCompanyInfoById(tPid, tName+" "+association, searchList, job)
				// 把查询完的一个企业按类别存起来
				for dk, dr := range dEnData {
					enInfo[dk] = append(enInfo[dk], dr...)
				}
			}
		}
	}

	return enInfo
}

// getCompanyInfoById 获取公司的详细的信息
func getCompanyInfoById(pid string, inFrom string, searchList []string, job _interface.ENScan) map[string][]gjson.Result {
	enData := make(map[string][]gjson.Result)
	res, enMap := job.GetCompanyBaseInfoById(pid)
	gologger.Info().Msgf("正在获取⌈%s⌋信息", res.Get(job.GetENMap()["enterprise_info"].Field[0]))
	// 增加企业信息
	enJsonTMP, _ := sjson.Set(res.Raw, "inFrom", inFrom)
	enData["enterprise_info"] = append(enData["enterprise_info"], gjson.Parse(enJsonTMP))
	// 适配风鸟
	if res.Get("orderNo").String() != "" {
		pid = res.Get("orderNo").String()
		fmt.Println(pid)
	}
	// 批量获取信息
	for _, sk := range searchList {
		s := enMap[sk]
		// 不支持这个搜索类型就跳过去
		if _, ok := enMap[sk]; !ok {
			continue
		}
		// 没有这个数据就跳过去，提高速度
		if s.Total <= 0 || s.Api == "" {
			gologger.Info().Str("type", sk).Msgf("GET ⌈%s⌋ 为空", s.Name)
			continue
		}

		// 判断结束调用获取数据接口
		listData, err := job.GetEnInfoList(pid, enMap[sk])
		if err != nil {
			gologger.Error().Msg(err.Error())
		}

		// 添加来源信息，并把信息存储到数据里面
		for _, y := range listData {
			valueTmp, _ := sjson.Set(y.Raw, "inFrom", inFrom)
			gs := gjson.Parse(valueTmp)
			enData[sk] = append(enData[sk], gs)
		}
		// 展示数据
		utils.TBS(s.KeyWord, s.Field, s.Name, listData)
	}
	return enData
}

// getAppById 直接使用关键词调用插件查询
func getAppByKeyWord(keyWord string, searchList []string, app _interface.App) (enInfo map[string][]gjson.Result) {
	enData := make(map[string][]gjson.Result)
	enMap := app.GetENMap()
	for _, sk := range searchList {
		if _, ok := enMap[sk]; !ok {
			continue
		}
		s := enMap[sk]
		gologger.Info().Msgf("正在获取⌈%s⌋信息", s.Name)
		listData := app.GetInfoList(keyWord, sk)
		enData[sk] = append(enData[sk], listData...)
		utils.TBS(s.KeyWord, s.Field, s.Name, listData)

	}
	return enData
}

func getAppById(rdata map[string][]map[string]string, searchList []string, app _interface.App) (enInfo map[string][]gjson.Result) {
	enData := make(map[string][]gjson.Result)
	enMap := app.GetENMap()
	for _, sk := range searchList {

		if _, ok := enMap[sk]; !ok {
			continue
		}
		s := enMap[sk]
		var enList []string
		// 获取需要的参数，比如企业名称、域名等
		// 0和1分别表示 ENSMapLN 的 key 和 value，定位出需要的数据
		// 暂时没遇到需要多种类型进行匹配的参数
		ap := s.AppParams
		for _, ens := range rdata[ap[0]] {
			enList = append(enList, ens[ap[1]])
		}
		// 对获取的目标进行去重
		utils.SetStr(enList)
		gologger.Info().Msgf("共获取到【%d】条，开始执行插件获取信息", len(enList))
		for i, v := range enList {
			gologger.Info().Msgf("正在获取第【%d】条数据 【%s】", i+1, v)
			listData := app.GetInfoList(v, sk)
			enData[sk] = append(enData[sk], listData...)
			utils.TBS(s.KeyWord, s.Field, s.Name, listData)
		}
	}
	return enData
}
