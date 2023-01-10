package main

import (
	"bytes"
	"embed"
	"fmt"
	"image/color"
	_ "image/png"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/text"
	logging "github.com/tsujio/game-logging-server/client"
	"github.com/tsujio/game-util/dotutil"
	"github.com/tsujio/game-util/resourceutil"
	"github.com/tsujio/game-util/touchutil"
)

const (
	gameName       = "cosmic-velocity"
	screenWidth    = 640
	screenHeight   = 480
	screenCenterX  = screenWidth / 2
	screenCenterY  = screenHeight / 2
	earthX, earthY = screenCenterX, screenCenterY
	earthM         = 100
	earthR         = 20
)

//go:embed resources/*.ttf resources/*.dat resources/bgm-*.wav resources/secret
var resources embed.FS

func loadAudioData(name string, audioContext *audio.Context) []byte {
	f, err := resources.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}

	return data
}

func createBGMPlayer(name string, audioContext *audio.Context) *audio.Player {
	f, err := resources.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}

	stream, err := wav.Decode(audioContext, bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}

	loop := audio.NewInfiniteLoop(stream, stream.Length())

	player, err := audio.NewPlayer(audioContext, loop)
	if err != nil {
		log.Fatal(err)
	}

	return player
}

var (
	largeFont, mediumFont, smallFont = (func() (l, m, s *resourceutil.Font) {
		l, m, s, err := resourceutil.LoadFont(resources, "resources/PressStart2P-Regular.ttf", nil)
		if err != nil {
			log.Fatal(err)
		}
		return
	})()
	audioContext       = audio.NewContext(48000)
	gameStartAudioData = loadAudioData("resources/魔王魂 効果音 システム49.mp3.dat", audioContext)
	gameOverAudioData  = loadAudioData("resources/魔王魂 効果音 システム32.mp3.dat", audioContext)
	rocketSetAudioData = loadAudioData("resources/魔王魂 効果音 システム14.mp3.dat", audioContext)
	hitAudioData       = loadAudioData("resources/魔王魂 効果音 システム16.mp3.dat", audioContext)
	bgmPlayer          = createBGMPlayer("resources/bgm-cosmic-velocity.wav", audioContext)
)

type rocket struct {
	x, y   float64
	vx, vy float64
	m      float64
	r      float64
}

var rocketPattern = dotutil.CreatePatternImage([][]int{
	{0, 0, 1, 0, 0},
	{0, 1, 1, 1, 0},
	{0, 1, 2, 1, 0},
	{0, 1, 1, 1, 0},
	{0, 1, 1, 1, 0},
	{0, 1, 1, 1, 0},
	{1, 1, 1, 1, 1},
	{1, 0, 1, 0, 1},
}, &dotutil.CreatePatternImageOption{
	ColorMap: map[int]color.Color{
		1: color.White,
		2: color.Black,
	},
})

var rocketFirePattern = dotutil.CreatePatternImage([][]int{
	{0, 1, 1, 1, 0},
	{1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1},
	{1, 1, 1, 1, 1},
	{1, 0, 1, 0, 1},
	{1, 0, 1, 0, 1},
	{1, 0, 1, 0, 1},
}, &dotutil.CreatePatternImageOption{
	Color: color.RGBA{0xff, 0, 0, 0xff},
})

func (r *rocket) draw(screen *ebiten.Image, game *Game) {
	_, h := rocketPattern.Size()
	dotutil.DrawImage(screen, rocketPattern, r.x, r.y, &dotutil.DrawImageOption{
		Scale:        r.r * 2 / float64(h),
		Rotate:       math.Atan2(r.vy, r.vx) + math.Pi/2,
		BasePosition: dotutil.DrawImagePositionCenter,
	})

	if game.hold {
		theta := math.Atan2(r.vy, r.vx)
		d := r.r * 2
		x := r.x + d*math.Cos(theta+math.Pi)
		y := r.y + d*math.Sin(theta+math.Pi)
		dotutil.DrawImage(screen, rocketFirePattern, x, y, &dotutil.DrawImageOption{
			Scale:        r.r * 2 / float64(h),
			Rotate:       theta + math.Pi/2,
			BasePosition: dotutil.DrawImagePositionCenter,
		})
	}
}

type meteoroid struct {
	x, y   float64
	vx, vy float64
	r      float64
	ticks  uint64
	orbit  [orbitLength][2]float64
}

const orbitLength = 300

var meteroidPattern = dotutil.CreatePatternImage([][]int{
	{0, 0, 1, 1, 1, 0, 0},
	{0, 1, 1, 1, 2, 1, 0},
	{1, 1, 1, 1, 2, 2, 1},
	{1, 1, 1, 1, 1, 2, 1},
	{1, 1, 1, 1, 1, 1, 1},
	{0, 1, 2, 2, 1, 1, 0},
	{0, 0, 1, 1, 1, 0, 0},
}, &dotutil.CreatePatternImageOption{
	ColorMap: map[int]color.Color{
		1: color.RGBA{0xbc, 0xb0, 0xa9, 0xff},
		2: color.RGBA{0xfc, 0xeb, 0xdd, 0xff},
	},
})

func (m *meteoroid) draw(screen *ebiten.Image, game *Game) {
	// Orbit
	var prev [2]float64
	for i := 0; i < int(math.Min(float64(m.ticks), orbitLength)); i++ {
		if i == 0 {
			prev = m.orbit[(m.ticks-uint64(i))%orbitLength]
			continue
		}
		if i%10 != 0 {
			continue
		}

		o := m.orbit[(m.ticks-uint64(i))%orbitLength]
		op := m.orbit[(m.ticks-uint64(i-1))%orbitLength]
		opp := m.orbit[(m.ticks-uint64(math.Max(float64(i-30), 0)))%orbitLength]

		if math.Abs(math.Atan2(o[1]-op[1], o[0]-op[0])-math.Atan2(o[1]-opp[1], o[0]-opp[0])) < math.Pi/24 {
			if i%30 != 0 {
				continue
			}
		}

		var c color.Color
		if i < orbitLength/6*1 {
			c = color.RGBA{0xff, 0x32, 0x2e, 0xff}
		} else if i < orbitLength/6*2 {
			c = color.RGBA{0xe0, 0x50, 0x19, 0xff}
		} else if i < orbitLength/6*3 {
			c = color.RGBA{0xff, 0x8a, 0x00, 0xff}
		} else if i < orbitLength/6*4 {
			c = color.RGBA{0xff, 0xc2, 0x1f, 0xff}
		} else if i < orbitLength/6*5 {
			c = color.RGBA{0xff, 0xe9, 0x1a, 0xff}
		} else {
			c = color.White
		}
		ebitenutil.DrawLine(screen, o[0], o[1], prev[0], prev[1], c)
		prev = o
	}

	// Body
	w, _ := meteroidPattern.Size()
	dotutil.DrawImage(screen, meteroidPattern, m.x, m.y, &dotutil.DrawImageOption{
		Scale:        m.r * 2 / float64(w),
		Rotate:       float64(m.ticks) / 180 * math.Pi * 2,
		BasePosition: dotutil.DrawImagePositionCenter,
	})
}

type effect struct {
	x, y   float64
	ticks  uint
	angles []float64
	plus   int
}

var effectPattern = dotutil.CreatePatternImage([][]int{{1}}, &dotutil.CreatePatternImageOption{
	Color: color.RGBA{0xbc, 0xb0, 0xa9, 0xff},
})

func (e *effect) draw(screen *ebiten.Image, game *Game) {
	for _, a := range e.angles {
		d := 0.5 * float64(e.ticks)
		w, _ := effectPattern.Size()
		dotutil.DrawImage(screen, effectPattern, e.x+d*math.Cos(a), e.y+d*math.Sin(a), &dotutil.DrawImageOption{
			Scale:        6.0 / float64(w),
			Rotate:       math.Pi * 2 * float64(e.ticks) / 120,
			BasePosition: dotutil.DrawImagePositionCenter,
		})
	}

	y := e.y - 15*math.Sin(math.Pi*float64(e.ticks)/60)
	text.Draw(screen, fmt.Sprintf("+%d", e.plus), mediumFont.Face, int(e.x), int(y), color.RGBA{0xf5, 0xc0, 0x01, 0xff})
}

type star struct {
	x, y float64
	r    float64
}

func (s *star) draw(screen *ebiten.Image, game *Game) {
	ebitenutil.DrawRect(screen, s.x, s.y, s.r*2, s.r*2, color.White)
}

type gameMode int

const (
	gameModeTitle gameMode = iota
	gameModePlaying
	gameModeGameOver
)

type Game struct {
	playerID           string
	playID             string
	mode               gameMode
	touchContext       *touchutil.TouchContext
	hold               bool
	ticksFromModeStart uint64
	rocket             *rocket
	meteoroids         []meteoroid
	effects            []effect
	stars              []star
	score              int
}

func (g *Game) calcGravity(x, y float64) (dvx, dvy float64) {
	d2 := math.Pow(earthX-x, 2) + math.Pow(earthY-y, 2)
	a := earthM / d2
	theta := math.Atan2(earthY-y, earthX-x)
	dvx = a * math.Cos(theta)
	dvy = a * math.Sin(theta)
	return
}

func (g *Game) Update() error {
	g.touchContext.Update()

	g.ticksFromModeStart++

	switch g.mode {
	case gameModeTitle:
		if g.touchContext.IsJustTouched() {
			g.mode = gameModePlaying
			g.ticksFromModeStart = 0

			logging.LogAsync(gameName, map[string]interface{}{
				"player_id": g.playerID,
				"play_id":   g.playID,
				"action":    "start_game",
			})

			audio.NewPlayerFromBytes(audioContext, gameStartAudioData).Play()

			bgmPlayer.Rewind()
			bgmPlayer.Play()
		}
	case gameModePlaying:
		if g.ticksFromModeStart%600 == 0 {
			logging.LogAsync(gameName, map[string]interface{}{
				"player_id": g.playerID,
				"play_id":   g.playID,
				"action":    "playing",
				"ticks":     g.ticksFromModeStart,
				"score":     g.score,
			})
		}

		if g.touchContext.IsJustTouched() {
			g.hold = true
		}
		if g.touchContext.IsJustReleased() {
			g.hold = false
		}

		// Effect ticks
		var newEffects []effect
		for i := 0; i < len(g.effects); i++ {
			e := &g.effects[i]
			e.ticks++
			if e.ticks < 60 {
				newEffects = append(newEffects, *e)
			}
		}
		g.effects = newEffects

		// Meteoroid enter
		if g.ticksFromModeStart%60 == 0 {
			w, h := screenWidth+50*2, screenHeight+50*2
			l := w*2 + h*2
			p := rand.Int() % l
			var x, y float64
			if p < w {
				x = float64(p)
				y = -50
			} else if p < w+h {
				x = screenWidth + 50
				y = float64(p - w)
			} else if p < w+h+w {
				x = float64(p - w - h)
				y = screenHeight + 50
			} else {
				x = -50
				y = float64(p - w - h - w)
			}
			theta := math.Atan2(earthY-y, earthX-x)
			for {
				if dt := (math.Pi*rand.Float64() - math.Pi/2); math.Abs(dt) > math.Pi/8 {
					theta += dt
					break
				}
			}
			v := 0.5
			g.meteoroids = append(g.meteoroids, meteoroid{
				x:     x,
				y:     y,
				vx:    v * math.Cos(theta),
				vy:    v * math.Sin(theta),
				r:     10,
				ticks: 0,
			})
		}

		// Rocket move
		g.rocket.x += g.rocket.vx
		g.rocket.y += g.rocket.vy

		// Rocket jet boost
		if g.hold {
			dv := 0.01
			theta := math.Atan2(g.rocket.vy, g.rocket.vx)
			g.rocket.vx += dv * math.Cos(theta)
			g.rocket.vy += dv * math.Sin(theta)
		}

		// Gravity
		dvx, dvy := g.calcGravity(g.rocket.x, g.rocket.y)
		g.rocket.vx += dvx
		g.rocket.vy += dvy
		if g.rocket.x < -50 || g.rocket.x > screenWidth+50 || g.rocket.y < -50 || g.rocket.y > screenHeight+50 {
			g.rocket = g.newRocket()

			audio.NewPlayerFromBytes(audioContext, rocketSetAudioData).Play()
		}
		for i := 0; i < len(g.meteoroids); i++ {
			m := &g.meteoroids[i]
			dvx, dvy := g.calcGravity(m.x, m.y)
			m.vx += dvx
			m.vy += dvy
		}

		// Meteoroids move
		var newMeteoroids []meteoroid
		for i := 0; i < len(g.meteoroids); i++ {
			m := &g.meteoroids[i]
			m.ticks++
			m.orbit[m.ticks%orbitLength] = [2]float64{m.x, m.y}
			m.x += m.vx
			m.y += m.vy

			if m.x > -50 && m.x < screenWidth+50 && m.y > -50 && m.y < screenHeight+50 {
				newMeteoroids = append(newMeteoroids, *m)
			}
		}
		g.meteoroids = newMeteoroids

		// Meteoroids collision
		newMeteoroids = []meteoroid{}
		for i := 0; i < len(g.meteoroids); i++ {
			m := &g.meteoroids[i]

			if math.Pow(m.x-g.rocket.x, 2)+math.Pow(m.y-g.rocket.y, 2) < math.Pow(m.r+g.rocket.r, 2) {
				g.rocket = g.newRocket()

				d := math.Sqrt(math.Pow(m.x-earthX, 2) + math.Pow(m.y-earthY, 2))
				var plus int
				if d < 100 {
					plus = 1
				} else if d < 150 {
					plus = 2
				} else {
					plus = 3
				}

				g.score += plus

				g.effects = append(g.effects, effect{
					x: m.x,
					y: m.y,
					angles: []float64{
						math.Pi * 2 * rand.Float64(),
						math.Pi * 2 * rand.Float64(),
						math.Pi * 2 * rand.Float64(),
						math.Pi * 2 * rand.Float64(),
						math.Pi * 2 * rand.Float64(),
						math.Pi * 2 * rand.Float64(),
					},
					plus: plus,
				})

				audio.NewPlayerFromBytes(audioContext, hitAudioData).Play()
			} else {
				newMeteoroids = append(newMeteoroids, *m)
			}

			if math.Pow(earthX-m.x, 2)+math.Pow(earthY-m.y, 2) < math.Pow(earthR+m.r, 2) {
				newMeteoroids = []meteoroid{*m}
				g.mode = gameModeGameOver

				audio.NewPlayerFromBytes(audioContext, gameOverAudioData).Play()

				logging.LogAsync(gameName, map[string]interface{}{
					"player_id": g.playerID,
					"play_id":   g.playID,
					"action":    "game_over",
					"ticks":     g.ticksFromModeStart,
					"score":     g.score,
				})

				break
			}
		}
		g.meteoroids = newMeteoroids

		// Rocket and earth collision
		if math.Pow(earthX-g.rocket.x, 2)+math.Pow(earthY-g.rocket.y, 2) < math.Pow(earthR+g.rocket.r, 2) {
			g.meteoroids = nil
			g.mode = gameModeGameOver

			audio.NewPlayerFromBytes(audioContext, gameOverAudioData).Play()

			logging.LogAsync(gameName, map[string]interface{}{
				"player_id": g.playerID,
				"play_id":   g.playID,
				"action":    "game_over",
				"ticks":     g.ticksFromModeStart,
				"score":     g.score,
			})
		}
	case gameModeGameOver:
		if g.touchContext.IsJustTouched() {
			g.initialize()
			bgmPlayer.Pause()
		}
	}

	return nil
}

var earthPattern = dotutil.CreatePatternImage([][]int{
	{0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 2, 2, 2, 1, 1, 1, 2, 2, 0, 0, 0, 0},
	{0, 0, 2, 2, 2, 2, 1, 1, 1, 1, 1, 2, 2, 2, 0, 0},
	{0, 0, 2, 3, 2, 2, 1, 1, 1, 1, 3, 1, 1, 2, 0, 0},
	{0, 2, 3, 2, 1, 1, 2, 1, 1, 3, 1, 1, 1, 1, 2, 0},
	{0, 2, 2, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 2, 0},
	{2, 2, 2, 1, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1},
	{2, 1, 1, 2, 3, 1, 1, 1, 1, 1, 1, 1, 1, 3, 1, 1},
	{1, 3, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 3, 1, 1, 1},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1},
	{0, 1, 1, 1, 1, 1, 3, 1, 1, 1, 1, 1, 1, 1, 2, 0},
	{0, 1, 1, 1, 2, 2, 1, 1, 1, 1, 1, 3, 1, 2, 2, 0},
	{0, 0, 1, 1, 2, 2, 1, 1, 1, 1, 3, 1, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0},
}, &dotutil.CreatePatternImageOption{
	ColorMap: map[int]color.Color{
		1: color.RGBA{0x01, 0x5f, 0xeb, 0xff},
		2: color.RGBA{0x01, 0xeb, 0x01, 0xff},
		3: color.White,
	},
})

func (g *Game) drawEarth(screen *ebiten.Image) {
	w, _ := earthPattern.Size()
	dotutil.DrawImage(screen, earthPattern, earthX, earthY, &dotutil.DrawImageOption{
		Scale:        earthR * 2 / float64(w),
		BasePosition: dotutil.DrawImagePositionCenter,
	})
}

func (g *Game) drawScore(screen *ebiten.Image) {
	scoreText := fmt.Sprintf("SCORE %d", g.score)
	text.Draw(screen, scoreText, smallFont.Face, screenWidth-(len(scoreText)+1)*int(smallFont.FaceOptions.Size), 15, color.White)
}

func (g *Game) drawTitle(screen *ebiten.Image) {
	titleText := []string{"COSMIC VELOCITY"}
	for i, s := range titleText {
		text.Draw(screen, s, largeFont.Face, screenCenterX-len(s)*int(largeFont.FaceOptions.Size)/2, 110+i*int(largeFont.FaceOptions.Size*1.8), color.White)
	}

	creditTexts := []string{"CREATOR: NAOKI TSUJIO", "FONT: Press Start 2P by CodeMan38", "SOUND: MaouDamashii"}
	for i, s := range creditTexts {
		text.Draw(screen, s, smallFont.Face, screenCenterX-len(s)*int(smallFont.FaceOptions.Size)/2, 400+i*int(smallFont.FaceOptions.Size*1.8), color.White)
	}
}

func (g *Game) drawGameOver(screen *ebiten.Image) {
	gameOverText := "GAME OVER"
	text.Draw(screen, gameOverText, largeFont.Face, screenCenterX-len(gameOverText)*int(largeFont.FaceOptions.Size)/2, 180, color.White)
	scoreText := []string{"YOUR SCORE IS", fmt.Sprintf("%d!", g.score)}
	for i, s := range scoreText {
		text.Draw(screen, s, mediumFont.Face, screenCenterX-len(s)*int(mediumFont.FaceOptions.Size)/2, 315+i*int(mediumFont.FaceOptions.Size*2), color.White)
	}
}

func (g *Game) Draw(screen *ebiten.Image) {
	switch g.mode {
	case gameModeTitle:
		for _, s := range g.stars {
			s.draw(screen, g)
		}
		g.drawEarth(screen)
		g.rocket.draw(screen, g)

		meteoroids := []*meteoroid{
			{
				x:  earthX - 180,
				y:  earthY - 51,
				vx: 0.6 * math.Cos(-math.Pi/4),
				vy: 0.6 * math.Sin(-math.Pi/4),
				r:  10,
			},
			{
				x:  earthX + 180,
				y:  earthY + 51,
				vx: 0.6 * math.Cos(math.Pi*3/4),
				vy: 0.6 * math.Sin(math.Pi*3/4),
				r:  10,
			},
		}
		for i := 0; i < orbitLength; i++ {
			for _, m := range meteoroids {
				m.ticks++
				m.orbit[m.ticks%orbitLength] = [2]float64{m.x, m.y}
				dvx, dvy := g.calcGravity(m.x, m.y)
				m.vx += dvx
				m.vy += dvy
				m.x += m.vx
				m.y += m.vy
			}
		}
		for _, m := range meteoroids {
			m.draw(screen, g)
		}

		g.drawTitle(screen)
	case gameModePlaying:
		for _, s := range g.stars {
			s.draw(screen, g)
		}
		g.drawEarth(screen)
		for _, m := range g.meteoroids {
			m.draw(screen, g)
		}
		g.rocket.draw(screen, g)
		for _, e := range g.effects {
			e.draw(screen, g)
		}
		g.drawScore(screen)
	case gameModeGameOver:
		for _, s := range g.stars {
			s.draw(screen, g)
		}
		g.drawEarth(screen)
		for _, m := range g.meteoroids {
			m.draw(screen, g)
		}
		g.rocket.draw(screen, g)
		for _, e := range g.effects {
			e.draw(screen, g)
		}
		g.drawScore(screen)
		g.drawGameOver(screen)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func (g *Game) newRocket() *rocket {
	r := earthR + 30
	v := math.Sqrt(float64(earthM) / float64(r))

	return &rocket{
		x:  earthX,
		y:  earthY - earthR - 30,
		vx: v,
		vy: 0,
		m:  1,
		r:  10,
	}
}

func (g *Game) initialize() {
	logging.LogAsync(gameName, map[string]interface{}{
		"player_id": g.playerID,
		"play_id":   g.playID,
		"action":    "initialize",
	})

	g.mode = gameModeTitle
	g.ticksFromModeStart = 0
	g.rocket = g.newRocket()
	g.meteoroids = []meteoroid{}
	g.effects = []effect{}
	var stars []star
	for i := 0; i < 100; i++ {
		stars = append(stars, star{
			x: screenWidth * rand.Float64(),
			y: screenHeight * rand.Float64(),
			r: 0.5 + rand.NormFloat64(),
		})
	}
	g.stars = stars
	g.hold = false
	g.score = 0
}

func main() {
	if os.Getenv("GAME_LOGGING") == "1" {
		secret, err := resources.ReadFile("resources/secret")
		if err == nil {
			logging.Enable(string(secret))
		}
	} else {
		logging.Disable()
	}

	if seed, err := strconv.Atoi(os.Getenv("GAME_RAND_SEED")); err == nil {
		rand.Seed(int64(seed))
	} else {
		rand.Seed(time.Now().Unix())
	}
	playerID := os.Getenv("GAME_PLAYER_ID")
	if playerID == "" {
		if playerIDObj, err := uuid.NewRandom(); err == nil {
			playerID = playerIDObj.String()
		}
	}

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("Cosmic Velocity")

	playIDObj, err := uuid.NewRandom()
	var playID string
	if err != nil {
		playID = "?"
	} else {
		playID = playIDObj.String()
	}

	game := &Game{
		playerID:     playerID,
		playID:       playID,
		touchContext: touchutil.CreateTouchContext(),
	}
	game.initialize()

	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
