package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strings"
)

const TOKEN = ""

type AllBuilds struct {
	Count    int    `json:"count"`
	Href     string `json:"href"`
	NextHref string `json:"nextHref"`
	Builds   []struct {
		ID            int    `json:"id"`
		BuildTypeId   string `json:buildTypeId`
		Number        string `json:number`
		Status        string `json:status`
		State         string `json:state`
		Composite     bool   `json:composite`
		BranchName    string `json:branchName`
		DefaultBranch bool   `json:defaultBranch`
		Href          string `json:href`
		WebUrl        string `json:webUrl`
	} `json:"build"`
}

type BuildData struct {
	Number string
	ID     int
	Href   string
}

type AllTestsResponse struct {
	Count          int    `json:"count"`
	Href           string `json:"href"`
	NextHref       string `json:"nextHref"`
	TestOccurrence []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Href   string `json:"href"`
	} `json:"testOccurrence"`
}

type TestStats struct {
	Name      string
	TotalRuns int
	Success   int
}

type TestData struct {
	Success bool
}

type FlakyStats struct {
	Percent float64
	Name    string
}

func doGetRequest(url string) []byte {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	request.Header.Add("Authorization", TOKEN)
	request.Header.Add("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		panic(err)
	}
	if err != nil {
		panic(err)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)

	return bodyBytes
}

// "https://teamcity.dgraph.io/app/rest/builds/?locator=branch:refs/heads/master,buildType:Dgraph_Ci" all builds that ran on draph master

// Fetch the status of all the tests that ran for the given buildId
func fetchTestsForBuild(buildID int, ch chan<- map[string]TestData) {
	url := fmt.Sprintf("https://teamcity.dgraph.io/app/rest/testOccurrences?locator=build:id:%d", buildID)
	testDataMap := make(map[string]TestData)
	for {
		bodyBytes := doGetRequest(url)
		var alltests AllTestsResponse
		err := json.Unmarshal(bodyBytes, &alltests)

		if err != nil {
			panic(err)
		}
		for i := 0; i < len(alltests.TestOccurrence); i++ {
			var testData TestData
			if alltests.TestOccurrence[i].Status == "SUCCESS" {
				testData.Success = true
			} else {
				testData.Success = false
			}
			testDataMap[alltests.TestOccurrence[i].Name] = testData
		}
		if len(alltests.NextHref) == 0 {
			break
		} else {
			url = fmt.Sprintf("https://teamcity.dgraph.io%s", alltests.NextHref)
		}
	}
	ch <- testDataMap
}

func fetchAllBuildsSince(date string) []BuildData {
	url := fmt.Sprintf("https://teamcity.dgraph.io/app/rest/builds/?locator=branch:refs/heads/master,buildType:Dgraph_Ci,sinceDate:%s", date)
	url = strings.ReplaceAll(url, "+", "%2B")
	var buildDatas []BuildData
	for {
		bodyBytes := doGetRequest(url)
		var allBuilds AllBuilds
		err := json.Unmarshal(bodyBytes, &allBuilds)

		if err != nil {
			panic(err)
		}

		for i := 0; i < len(allBuilds.Builds); i++ {
			var buildData BuildData
			buildData.Href = allBuilds.Builds[i].Href
			buildData.Number = allBuilds.Builds[i].Number
			buildData.ID = allBuilds.Builds[i].ID
			buildDatas = append(buildDatas, buildData)
		}
		if len(allBuilds.NextHref) == 0 {
			break
		} else {
			url = fmt.Sprintf("https://teamcity.dgraph.io%s", allBuilds.NextHref)
		}
	}

	return buildDatas
}

func main() {
	since := "20200829T225042+0000"
	buildDataList := fetchAllBuildsSince(since)

	// Get the tests that ran on the last build
	if len(buildDataList) == 0 {
		log.Fatalln("No builds found")
	}
	ch := make(chan map[string]TestData)
	go fetchTestsForBuild(buildDataList[0].ID, ch)
	testsMap := <-ch
	testStatsMap := make(map[string]TestStats)
	for k := range testsMap {
		var testStats TestStats
		testStats.Name = k
		if testsMap[k].Success {
			testStats.Success++
		}
		testStats.TotalRuns++
		testStatsMap[k] = testStats
	}
	// Find the tests that fail the most percentage wise and output top 10
	for i := 1; i < len(buildDataList); i++ {
		go fetchTestsForBuild(buildDataList[i].ID, ch)
	}
	for i := 1; i < len(buildDataList); i++ {
		currentTestsMap := <-ch
		for k := range testsMap {
			test, found := currentTestsMap[k]
			if !found {
				continue
			}
			var temp = testStatsMap[k]
			if test.Success {
				temp.Success++
			}
			temp.TotalRuns++
			testStatsMap[k] = temp
		}
	}

	var mostFlaky []FlakyStats
	for k := range testsMap {
		var flakyStats FlakyStats
		flakyStats.Name = k
		flakyStats.Percent = (float64(testStatsMap[k].TotalRuns-testStatsMap[k].Success) / float64(testStatsMap[k].TotalRuns))
		mostFlaky = append(mostFlaky, flakyStats)
	}
	sort.Slice(mostFlaky, func(i, j int) bool {
		return mostFlaky[i].Percent > mostFlaky[j].Percent
	})

	for i := 0; i < 50; i++ {
		temp := testStatsMap[mostFlaky[i].Name]
		fmt.Printf("%s %d %d %f \n", mostFlaky[i].Name, temp.Success, temp.TotalRuns, mostFlaky[i].Percent)
	}
}

// func fetchTestStats(name string) TestStats {
// 	println(name)
// 	url := fmt.Sprintf("https://teamcity.dgraph.io/app/rest/testOccurrences?locator=test:(name:%s)", name)
// 	url = strings.ReplaceAll(url, " ", "%20")
// 	getAllTestsRuns, err := http.NewRequest("GET", url, nil)
// 	if err != nil {
// 		panic(err)
// 	}

// 	getAllTestsRuns.Header.Add("Authorization", TOKEN)
// 	getAllTestsRuns.Header.Add("Accept", "application/json")

// 	client := &http.Client{}
// 	resp, err := client.Do(getAllTestsRuns)
// 	if err != nil {
// 		panic(err)
// 	}
// 	bodyBytes, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		panic(err)
// 	}

// 	// bodyNew := strings.ReplaceAll(bodyString, "<nil>", "nil")

// 	var alltests AllTestsResponse
// 	err = json.Unmarshal(bodyBytes, &alltests)
// 	if err != nil {
// 		panic(err)
// 	}
// 	var testStats TestStats
// 	testStats.TotalRuns = 0
// 	testStats.Success = 0
// 	for i := 0; i < len(alltests.TestOccurrence); i++ {
// 		if alltests.TestOccurrence[i].Status == "SUCCESS" {
// 			testStats.Success++
// 		}
// 		testStats.TotalRuns++
// 	}
// 	testStats.Name = name

// 	return testStats
// }

// func fetchAllTests(url string) []string {
// 	getAllTestsRequest, err := http.NewRequest("GET", url, nil)
// 	if err != nil {
// 		panic(err)
// 	}

// 	getAllTestsRequest.Header.Add("Authorization", TOKEN)
// 	getAllTestsRequest.Header.Add("Accept", "application/json")

// 	client := &http.Client{}
// 	resp, err := client.Do(getAllTestsRequest)
// 	if err != nil {
// 		panic(err)
// 	}
// 	bodyBytes, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		panic(err)
// 	}
// 	var alltests AllTestsResponse
// 	err = json.Unmarshal(bodyBytes, &alltests)
// 	if err != nil {
// 		panic(err)
// 	}
// 	var names []string
// 	for i := 0; i < len(alltests.TestOccurrence); i++ {
// 		names = append(names, alltests.TestOccurrence[i].Name)
// 	}

// 	return names
// }
