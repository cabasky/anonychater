package hub

import (
	"hash/fnv"
	"log"
	"sort"
	"sync"

	dicebear "github.com/dicebear/dicebear-go/v10"
	"github.com/dicebear/styles/v10"
)

var (
	avatarStyles     map[string]*dicebear.Style
	avatarStyleNames []string
	avatarCache      sync.Map
)

func init() {
	avatarStyles = make(map[string]*dicebear.Style)

	rawMap := map[string]string{
		"bottts-neutral": styles.BotttsNeutral,
		"fun-emoji":      styles.FunEmoji,
		"initial-face":   styles.InitialFace,
		"gaze":           styles.Gaze,
		"voxel-bot":      styles.VoxelBot,
		"sprouts":        styles.Sprouts,
		"thumbs":         styles.Thumbs,
	}

	for name, raw := range rawMap {
		style, err := dicebear.NewStyle([]byte(raw))
		if err != nil {
			log.Printf("dicebear style %s failed: %v", name, err)
			continue
		}
		avatarStyles[name] = style
		avatarStyleNames = append(avatarStyleNames, name)
	}

	sort.Strings(avatarStyleNames)
	debugf("avatar styles loaded: %v", avatarStyleNames)
}

func GenerateAvatar(userID string) string {
	if cached, ok := avatarCache.Load(userID); ok {
		return cached.(string)
	}
	if len(avatarStyleNames) == 0 {
		return ""
	}

	h := fnv.New32a()
	h.Write([]byte(userID))
	idx := h.Sum32() % uint32(len(avatarStyleNames))
	styleName := avatarStyleNames[idx]

	style, ok := avatarStyles[styleName]
	if !ok {
		return ""
	}

	avatar, err := dicebear.NewAvatar(style, map[string]any{"seed": userID})
	if err != nil {
		log.Printf("generate avatar %s: %v", userID, err)
		return ""
	}
	svg := avatar.SVG()
	avatarCache.Store(userID, svg)
	return svg
}

// AvatarMap 为一批 userID 预生成头像映射，供导出使用。
func AvatarMap(userIDs []string) map[string]string {
	m := make(map[string]string, len(userIDs))
	for _, id := range userIDs {
		if _, ok := m[id]; !ok {
			m[id] = GenerateAvatar(id)
		}
	}
	return m
}

// RandomNickname 组合“修饰语 + 中心语”。
// 词库里的 modifier 已经自带"的"，直接拼接即可。
func RandomNickname(modifier, noun string) string {
	if modifier == "" || noun == "" {
		return ""
	}
	return modifier + noun
}