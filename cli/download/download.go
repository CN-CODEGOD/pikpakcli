package download

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/52funny/pikpakcli/conf"
	"github.com/52funny/pikpakcli/internal/api"
	"github.com/52funny/pikpakcli/internal/aria2"
	"github.com/52funny/pikpakcli/internal/logx"
	"github.com/52funny/pikpakcli/internal/utils"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var DownloadCmd = &cobra.Command{
	Use:     "download",
	Aliases: []string{"d"},
	Short:   `Download files from PikPak (supports aria2)`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help()
			return
		}
		p := api.NewPikPakWithContext(cmd.Context(), conf.Config.Username, conf.Config.Password)
		err := p.Login()
		if err != nil {
			fmt.Println("Login failed")
			logx.Error(err)
			return
		}
		args, err = api.ExpandRemotePatterns(&p, folder, args, false)
		if err != nil {
			fmt.Println("Expand download target failed")
			logx.Error(err)
			return
		}
		handleDownload(cmd, &p, args)
	},
}

var count int
var folder string
var parentId string
var output string
var progress bool
var timeRangeSpec string
var useAria2 bool
var disableAria2 bool
var aria2RPCUrl string
var aria2Secret string
var aria2Dir string
var exportAria2Input string

var clipper timeClipper = ffmpegClipper{}

type warpFile struct {
	f      *api.File
	output string
}

type warpStat struct {
	s      api.FileStat
	output string
}

const progressNameMaxRunes = 36

func init() {
	DownloadCmd.Flags().IntVarP(&count, "count", "c", 1, "number of simultaneous downloads (built-in mode)")
	DownloadCmd.Flags().StringVarP(&output, "output", "o", ".", "output directory")
	DownloadCmd.Flags().StringVarP(&folder, "path", "p", "/", "specific the base path on the pikpak server")
	DownloadCmd.Flags().StringVarP(&parentId, "parent-id", "P", "", "the parent path id")
	DownloadCmd.Flags().BoolVarP(&progress, "progress", "g", false, "show download progress (built-in mode)")
	DownloadCmd.Flags().StringVar(&timeRangeSpec, "time-range", "", "download a time range from a single video file (built-in, requires ffmpeg)")
	DownloadCmd.Flags().BoolVar(&useAria2, "aria2", false, "force use aria2 for downloading")
	DownloadCmd.Flags().BoolVar(&disableAria2, "no-aria2", false, "disable aria2, use built-in HTTP downloader")
	DownloadCmd.Flags().StringVar(&aria2RPCUrl, "aria2-rpc-url", "", "aria2 JSON-RPC URL (overrides config)")
	DownloadCmd.Flags().StringVar(&aria2Secret, "aria2-secret", "", "aria2 RPC secret (overrides config)")
	DownloadCmd.Flags().StringVar(&aria2Dir, "aria2-dir", "", "aria2 download directory (overrides config)")
	DownloadCmd.Flags().StringVar(&exportAria2Input, "export-aria2", "", "export aria2 input file instead of downloading")
}

type downloadTargetResolver interface {
	GetFileByPath(path string) (api.FileStat, error)
	GetFileStat(parentId string, name string) (api.FileStat, error)
	GetPathFolderId(dirPath string) (string, error)
}

func handleDownload(cmd *cobra.Command, p *api.PikPak, args []string) {
	if strings.TrimSpace(timeRangeSpec) != "" {
		downloadTimeRangeTarget(p, args)
		return
	}

	if exportAria2Input != "" {
		exportAria2InputFile(p, args)
		return
	}

	if shouldUseAria2() {
		handleAria2Download(p, args)
		return
	}

	if err := utils.CreateDirIfNotExist(output); err != nil {
		fmt.Println("Create output directory failed")
		logx.Error(err)
		return
	}

	if requiresExplicitOutputFlag(cmd, args) {
		fmt.Println("Use -o to specify the output directory when downloading specific files")
		return
	}

	for _, arg := range args {
		downloadTarget(p, arg)
	}
}

func shouldUseAria2() bool {
	if disableAria2 {
		return false
	}
	if useAria2 {
		return true
	}
	rpcURL := getAria2RPCURL()
	secret := getAria2Secret()
	client := aria2.NewClient(rpcURL, secret)
	_, err := client.GetVersion()
	if err != nil {
		logx.Debugln("aria2", "aria2 not available, falling back to built-in downloader:", err)
		return false
	}
	return true
}

func getAria2RPCURL() string {
	if aria2RPCUrl != "" {
		return aria2RPCUrl
	}
	return conf.Config.Aria2.GetRPCUrl()
}

func getAria2Secret() string {
	if aria2Secret != "" {
		return aria2Secret
	}
	return conf.Config.Aria2.Secret
}

func getAria2DownloadDir() string {
	if aria2Dir != "" {
		return aria2Dir
	}
	if conf.Config.Aria2.DownloadDir != "" {
		return conf.Config.Aria2.DownloadDir
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return "."
	}
	return absOutput
}

func requiresExplicitOutputFlag(cmd *cobra.Command, args []string) bool {
	if cmd == nil || cmd.Flags().Changed("output") || len(args) <= 1 {
		return false
	}
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "." || trimmed == ".." {
			return true
		}
	}
	return false
}

// --- Aria2 download mode ---

func handleAria2Download(p *api.PikPak, args []string) {
	rpcURL := getAria2RPCURL()
	secret := getAria2Secret()
	client := aria2.NewClient(rpcURL, secret)

	version, err := client.GetVersion()
	if err != nil {
		fmt.Println("Connect to aria2 failed:", err)
		logx.Error(err)
		fmt.Println("Falling back to built-in downloader...")
		disableAria2 = true
		handleDownload(nil, p, args)
		return
	}
	fmt.Printf("Connected to aria2 v%s\n", version["version"])

	dlDir := getAria2DownloadDir()
	var gids []aria2Task

	for _, arg := range args {
		stat, err := resolveDownloadTarget(p, arg)
		if err != nil {
			target := remoteTargetPath(arg)
			fmt.Println("Resolve download target failed:", target)
			logx.Error(err)
			continue
		}

		if stat.Kind == api.FileKindFolder {
			folderGids := addFolderToAria2(p, client, stat.ID, stat.Name, dlDir)
			gids = append(gids, folderGids...)
		} else {
			file, err := p.GetFile(stat.ID)
			if err != nil {
				fmt.Println("Get file failed:", stat.Name)
				logx.Error(err)
				continue
			}
			gid := addFileToAria2(client, &file, dlDir, "")
			if gid != "" {
				gids = append(gids, aria2Task{gid: gid, name: file.Name})
			}
		}
	}

	if len(gids) == 0 {
		fmt.Println("No files to download")
		return
	}

	fmt.Printf("\nSubmitted %d file(s) to aria2\n", len(gids))
	fmt.Println("Use 'aria2c --show-files' or web UI to monitor download progress")

	stat, err := client.GetGlobalStat()
	if err == nil {
		fmt.Printf("aria2 status: active=%s waiting=%s stopped=%s\n",
			stat.NumActive, stat.NumWaiting, stat.NumStopped)
	}
}

type aria2Task struct {
	gid  string
	name string
}

func addFolderToAria2(p *api.PikPak, client *aria2.Client, folderID, folderName, baseDir string) []aria2Task {
	var tasks []aria2Task
	collectStat := make([]warpStat, 0)
	recursiveCollect(p, &collectStat, folderID, folderName)

	for _, ws := range collectStat {
		file, err := p.GetFile(ws.s.ID)
		if err != nil {
			fmt.Println("Get file failed:", ws.s.Name)
			logx.Error(err)
			continue
		}
		gid := addFileToAria2(client, &file, baseDir, ws.output)
		if gid != "" {
			tasks = append(tasks, aria2Task{gid: gid, name: file.Name})
		}
	}
	return tasks
}

func addFileToAria2(client *aria2.Client, file *api.File, baseDir, subDir string) string {
	downloadURL := getDownloadURL(file)
	if downloadURL == "" {
		fmt.Println("No download URL for:", file.Name)
		return ""
	}

	dir := baseDir
	if subDir != "" {
		dir = filepath.Join(baseDir, subDir)
	}

	options := map[string]interface{}{
		"dir":               dir,
		"out":               file.Name,
		"max-connection-per-server": conf.Config.Aria2.GetMaxConnections(),
		"split":             conf.Config.Aria2.GetSplit(),
		"min-split-size":    conf.Config.Aria2.GetMinSplitSize(),
		"header":            fmt.Sprintf("User-Agent: %s", api.UserAgent),
	}

	gid, err := client.AddURI([]string{downloadURL}, options)
	if err != nil {
		fmt.Println("Add to aria2 failed:", file.Name, "-", err)
		logx.Error(err)
		return ""
	}

	fmt.Printf("[aria2] %s -> %s (GID: %s)\n", file.Name, dir, gid)
	return gid
}

func getDownloadURL(file *api.File) string {
	url := strings.TrimSpace(file.Links.ApplicationOctetStream.URL)
	if url != "" {
		return url
	}
	for _, media := range file.Medias {
		if media.IsDefault && media.IsVisible {
			u := strings.TrimSpace(media.Link.URL)
			if u != "" {
				return u
			}
		}
	}
	for _, media := range file.Medias {
		if media.IsVisible {
			u := strings.TrimSpace(media.Link.URL)
			if u != "" {
				return u
			}
		}
	}
	for _, media := range file.Medias {
		u := strings.TrimSpace(media.Link.URL)
		if u != "" {
			return u
		}
	}
	return ""
}

// --- Export aria2 input file ---

func exportAria2InputFile(p *api.PikPak, args []string) {
	f, err := os.Create(exportAria2Input)
	if err != nil {
		fmt.Println("Create export file failed:", err)
		logx.Error(err)
		return
	}
	defer f.Close()

	count := 0
	for _, arg := range args {
		stat, err := resolveDownloadTarget(p, arg)
		if err != nil {
			target := remoteTargetPath(arg)
			fmt.Println("Resolve download target failed:", target)
			logx.Error(err)
			continue
		}

		if stat.Kind == api.FileKindFolder {
			n := exportFolderAria2Input(p, f, stat.ID, stat.Name)
			count += n
		} else {
			file, err := p.GetFile(stat.ID)
			if err != nil {
				fmt.Println("Get file failed:", stat.Name)
				logx.Error(err)
				continue
			}
			exportFileAria2Input(f, &file, "")
			count++
		}
	}

	fmt.Printf("Exported %d file(s) to %s\n", count, exportAria2Input)
}

func exportFolderAria2Input(p *api.PikPak, f *os.File, folderID, folderName string) int {
	collectStat := make([]warpStat, 0)
	recursiveCollect(p, &collectStat, folderID, folderName)

	count := 0
	for _, ws := range collectStat {
		file, err := p.GetFile(ws.s.ID)
		if err != nil {
			fmt.Println("Get file failed:", ws.s.Name)
			logx.Error(err)
			continue
		}
		exportFileAria2Input(f, &file, ws.output)
		count++
	}
	return count
}

func exportFileAria2Input(f *os.File, file *api.File, subDir string) {
	downloadURL := getDownloadURL(file)
	if downloadURL == "" {
		fmt.Println("No download URL for:", file.Name)
		return
	}

	fmt.Fprintf(f, "%s\n", downloadURL)
	fmt.Fprintf(f, "  out=%s\n", file.Name)
	if subDir != "" {
		fmt.Fprintf(f, "  dir=%s\n", subDir)
	}
	fmt.Fprintf(f, "  header=User-Agent: %s\n", api.UserAgent)
	fmt.Fprintf(f, "  max-connection-per-server=%d\n", conf.Config.Aria2.GetMaxConnections())
	fmt.Fprintf(f, "  split=%d\n", conf.Config.Aria2.GetSplit())
	fmt.Fprintf(f, "  min-split-size=%s\n", conf.Config.Aria2.GetMinSplitSize())
	fmt.Fprintln(f)
}

// --- Built-in download mode ---

func downloadTimeRangeTarget(p *api.PikPak, args []string) {
	if len(args) != 1 {
		fmt.Println("Time range download requires exactly one file target")
		return
	}

	timeRange, err := parseTimeRangeSpec(timeRangeSpec)
	if err != nil {
		fmt.Println("Parse time range failed:", err)
		return
	}

	stat, err := resolveDownloadTarget(p, args[0])
	if err != nil {
		target := remoteTargetPath(args[0])
		fmt.Println("Resolve download target failed:", target)
		logx.Error(err)
		return
	}
	if stat.Kind == api.FileKindFolder {
		fmt.Println("Time range download only supports file targets")
		return
	}

	if err := utils.CreateDirIfNotExist(output); err != nil {
		fmt.Println("Create output directory failed")
		logx.Error(err)
		return
	}

	file := mustGetFile(p, stat)
	sourceURL := mediaClipSourceURL(file)
	if sourceURL == "" {
		fmt.Println("Time range download failed: file has no playable media URL")
		return
	}

	path := filepath.Join(output, timeRangeOutputName(file.Name, timeRange))
	if err := clipper.Clip(sourceURL, timeRange, path); err != nil {
		fmt.Println("Time range download failed:", file.Name)
		logx.Error(err)
		return
	}
	fmt.Println("Download", file.Name, "Success")
}

type TimeRange struct {
	Start string
	End   string
}

type timeClipper interface {
	Clip(sourceURL string, timeRange TimeRange, outputPath string) error
}

type ffmpegClipper struct{}

func (ffmpegClipper) Clip(sourceURL string, timeRange TimeRange, outputPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg is required for --time-range; install ffmpeg and make sure it is in PATH")
	}

	args := []string{
		"-y",
		"-ss", timeRange.Start,
	}
	if timeRange.End != "" {
		args = append(args, "-to", timeRange.End)
	}
	args = append(args, "-i", sourceURL, "-c", "copy", outputPath)

	cmd := exec.Command(ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func parseTimeRangeSpec(spec string) (TimeRange, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return TimeRange{}, fmt.Errorf("time range must not be empty")
	}

	parts := strings.Split(trimmed, "-")
	if len(parts) != 2 {
		return TimeRange{}, fmt.Errorf("time range must be START-END or START-")
	}
	if strings.TrimSpace(parts[0]) == "" {
		return TimeRange{}, fmt.Errorf("time range start must not be empty")
	}

	start := strings.TrimSpace(parts[0])
	end := strings.TrimSpace(parts[1])
	if err := validateTimePoint(start); err != nil {
		return TimeRange{}, fmt.Errorf("invalid time range start: %w", err)
	}
	if end != "" {
		if err := validateTimePoint(end); err != nil {
			return TimeRange{}, fmt.Errorf("invalid time range end: %w", err)
		}
	}
	return TimeRange{Start: start, End: end}, nil
}

func validateTimePoint(value string) error {
	if value == "" {
		return fmt.Errorf("time point must not be empty")
	}
	for _, part := range strings.Split(value, ":") {
		if part == "" {
			return fmt.Errorf("time point contains an empty component")
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("time point components must be non-negative integers")
		}
	}
	return nil
}

func mediaClipSourceURL(file *api.File) string {
	for _, media := range file.Medias {
		if media.IsDefault && media.IsVisible && strings.TrimSpace(media.Link.URL) != "" {
			return media.Link.URL
		}
	}
	for _, media := range file.Medias {
		if media.IsVisible && strings.TrimSpace(media.Link.URL) != "" {
			return media.Link.URL
		}
	}
	for _, media := range file.Medias {
		if strings.TrimSpace(media.Link.URL) != "" {
			return media.Link.URL
		}
	}
	return strings.TrimSpace(file.Links.ApplicationOctetStream.URL)
}

func timeRangeOutputName(name string, timeRange TimeRange) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	suffix := strings.ReplaceAll(timeRange.Start, ":", "-") + "-"
	if timeRange.End != "" {
		suffix += strings.ReplaceAll(timeRange.End, ":", "-")
	} else {
		suffix += "end"
	}
	return base + "." + suffix + ext
}

func downloadTarget(p *api.PikPak, arg string) {
	stat, err := resolveDownloadTarget(p, arg)
	if err != nil {
		target := remoteTargetPath(arg)
		fmt.Println("Resolve download target failed:", target)
		logx.Error(err)
		return
	}

	if stat.Kind == api.FileKindFolder {
		downloadFolder(p, stat.ID, localOutputRoot(stat.Name))
		return
	}

	downloadFiles(p, []warpFile{
		{
			f:      mustGetFile(p, stat),
			output: output,
		},
	})
}

func downloadFolder(p *api.PikPak, folderID string, rootOutput string) {
	collectStat := make([]warpStat, 0)
	recursiveCollect(p, &collectStat, folderID, rootOutput)
	downloadStats(p, collectStat)
}

func recursiveCollect(p *api.PikPak, collectWarpFile *[]warpStat, parentId string, parentPath string) {
	statList, err := p.GetFolderFileStatList(parentId)
	if err != nil {
		fmt.Println("Get folder file stat list failed")
		logx.Error(err)
		return
	}
	for _, r := range statList {
		if r.Kind == api.FileKindFolder {
			recursiveCollect(p, collectWarpFile, r.ID, filepath.Join(parentPath, r.Name))
		} else {
			*collectWarpFile = append(*collectWarpFile, warpStat{
				s:      r,
				output: parentPath,
			})
		}
	}
}

func downloadStats(p *api.PikPak, collectStat []warpStat) {
	if len(collectStat) == 0 {
		return
	}

	statCh := make(chan warpStat, len(collectStat))
	statDone := make(chan struct{})

	fileCh := make(chan warpFile, len(collectStat))
	fileDone := make(chan struct{})

	for i := 0; i < 4; i += 1 {
		go func(fileCh chan<- warpFile, statCh <-chan warpStat, statDone chan<- struct{}) {
			for {
				stat, ok := <-statCh
				if !ok {
					break
				}
				file, err := p.GetFile(stat.s.ID)
				if err != nil {
					fmt.Println("Get file failed")
					logx.Error(err)
				}
				fileCh <- warpFile{
					f:      &file,
					output: stat.output,
				}
				statDone <- struct{}{}
			}
		}(fileCh, statCh, statDone)
	}

	pb := startDownloadWorkers(fileCh, fileDone)

	for i := 0; i < len(collectStat); i += 1 {
		err := utils.CreateDirIfNotExist(collectStat[i].output)
		if err != nil {
			fmt.Println("Create output directory failed")
			logx.Error(err)
			return
		}
		statCh <- collectStat[i]
	}
	close(statCh)

	for i := 0; i < len(collectStat); i += 1 {
		<-statDone
	}
	close(statDone)

	for i := 0; i < len(collectStat); i += 1 {
		<-fileDone
	}
	if pb != nil {
		pb.Wait()
	}
}

func downloadFiles(p *api.PikPak, files []warpFile) {
	sendCh := make(chan warpFile, len(files))
	receiveCh := make(chan struct{}, len(files))
	pb := startDownloadWorkers(sendCh, receiveCh)
	for _, file := range files {
		sendCh <- file
	}
	close(sendCh)
	for i := 0; i < len(files); i++ {
		<-receiveCh
	}
	close(receiveCh)
	if pb != nil {
		pb.Wait()
	}
}

func startDownloadWorkers(sendCh <-chan warpFile, receiveCh chan<- struct{}) *mpb.Progress {
	var pb *mpb.Progress
	if progress {
		pb = mpb.New(
			mpb.WithWidth(30),
			mpb.WithAutoRefresh(),
		)
	}

	for i := 0; i < count; i++ {
		go download(sendCh, receiveCh, pb)
	}

	return pb
}

func resolveDownloadTarget(p downloadTargetResolver, arg string) (api.FileStat, error) {
	if target := strings.TrimSpace(arg); target == "" {
		if parentId != "" {
			return api.FileStat{
				Kind: api.FileKindFolder,
				ID:   parentId,
				Name: filepath.Base(filepath.Clean(folder)),
			}, nil
		}
		remotePath := remoteTargetPath("")
		if remotePath == string(filepath.Separator) {
			id, err := p.GetPathFolderId(folder)
			if err != nil {
				return api.FileStat{}, err
			}
			return api.FileStat{
				Kind: api.FileKindFolder,
				ID:   id,
				Name: "",
			}, nil
		}
		return p.GetFileByPath(remotePath)
	}

	if parentId != "" && !filepath.IsAbs(arg) && !strings.Contains(arg, string(filepath.Separator)) {
		return p.GetFileStat(parentId, arg)
	}

	return p.GetFileByPath(remoteTargetPath(arg))
}

func remoteTargetPath(arg string) string {
	base := strings.TrimSpace(folder)
	target := strings.TrimSpace(arg)
	if target == "" {
		target = "."
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(string(filepath.Separator), base, target))
}

func localOutputRoot(name string) string {
	if strings.TrimSpace(name) == "" || name == string(filepath.Separator) || name == "." {
		return output
	}
	return filepath.Join(output, name)
}

func mustGetFile(p *api.PikPak, stat api.FileStat) *api.File {
	file, err := p.GetFile(stat.ID)
	if err != nil {
		fmt.Println("Get file failed")
		logx.Error(err)
		return &api.File{FileStat: stat}
	}
	return &file
}

func progressDisplayName(warp warpFile) string {
	name := warp.f.Name
	if base := filepath.Base(filepath.Clean(warp.output)); base != "." && base != string(filepath.Separator) && base != "" {
		name = filepath.Join(base, name)
	}
	return trimRunes(name, progressNameMaxRunes)
}

func trimRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func download(inCh <-chan warpFile, out chan<- struct{}, pb *mpb.Progress) {
	for {
		warp, ok := <-inCh
		if !ok {
			break
		}

		path := filepath.Join(warp.output, warp.f.Name)
		exist, err := utils.Exists(path)
		if err != nil {
			out <- struct{}{}
			continue
		}
		flag := path + ".pikpakidmdownload"
		hasFlag, err := utils.Exists(flag)
		if err != nil {
			out <- struct{}{}
			continue
		}
		if exist && !hasFlag {
			out <- struct{}{}
			continue
		}
		err = utils.TouchFile(flag)
		if err != nil {
			out <- struct{}{}
			continue
		}

		siz, err := strconv.ParseInt(warp.f.Size, 10, 64)
		if err != nil {
			out <- struct{}{}
			continue
		}

		var bar *mpb.Bar

		if pb != nil {
			bar = pb.AddBar(siz,
				mpb.PrependDecorators(
					decor.Name(progressDisplayName(warp), decor.WC{W: progressNameMaxRunes + 2, C: decor.DSyncWidth}),
					decor.CountersKibiByte("% .1f / % .1f", decor.WCSyncSpace),
					decor.Percentage(decor.WCSyncSpace),
				),
				mpb.AppendDecorators(
					decor.Name(" | ", decor.WCSyncSpace),
					decor.Name("ETA ", decor.WCSyncSpace),
					decor.EwmaETA(decor.ET_STYLE_GO, 30),
					decor.Name(" | ", decor.WCSyncSpace),
					decor.Name("SPD ", decor.WCSyncSpace),
					decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 60),
				),
			)
		}

		err = warp.f.Download(path, bar)
		if err == nil {
			if pb == nil {
				fmt.Println("Download", warp.f.Name, "Success")
			}
			os.Remove(flag)
			if bar != nil {
				bar.SetTotal(siz, true)
			}
		} else {
			if pb == nil {
				fmt.Println("Download failed:", warp.f.Name)
				logx.Error(err)
			}
			if bar != nil {
				bar.Abort(false)
			}
		}
		out <- struct{}{}
	}
}
