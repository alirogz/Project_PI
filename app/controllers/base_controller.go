package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/alirogz/goshop/app/models"
	"github.com/alirogz/goshop/database/seeders"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/unrolled/render"
	"github.com/urfave/cli"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Server struct {
	DB        *gorm.DB
	Router    *mux.Router
	AppConfig *AppConfig
}

type AppConfig struct {
	AppName string
	AppEnv  string
	AppPort string
	AppURL  string
}

type DBConfig struct {
	DBHost     string
	DBUser     string
	DBPassword string
	DBName     string
	DBPort     string
	DBDriver   string
}

type PageLink struct {
	Page          int32
	Url           string
	IsCurrentPage bool
}

type PaginationLinks struct {
	CurrentPage string
	NextPage    string
	PrevPage    string
	TotalRows   int32
	TotalPages  int32
	Links       []PageLink
}

type PaginationParams struct {
	Path        string
	TotalRows   int32
	PerPage     int32
	CurrentPage int32
}

type Result struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
}

var store *sessions.CookieStore

var sessionShoppingCart = "shopping-cart-session"
var sessionFlash = "flash-session"
var sessionUser = "user-session"

func initSessionStore() {
	key := os.Getenv("SESSION_KEY")
	if key == "" {
		// fallback dev; untuk production WAJIB isi SESSION_KEY di .env
		key = "dev-secret-change-me"
	}
	store = sessions.NewCookieStore([]byte(key))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 hari
		HttpOnly: true,
		// Secure:   true,                 // aktifkan kalau sudah HTTPS
		// SameSite: http.SameSiteLaxMode, // opsi aman default
	}
}

func (server *Server) Initialize(appConfig AppConfig, dbConfig DBConfig) {
	fmt.Println("Welcome to " + appConfig.AppName)

	server.initializeDB(dbConfig)
	server.initializeAppConfig(appConfig)
	initSessionStore()
	server.initializeRoutes()
}

func (server *Server) Run(addr string) {
	fmt.Printf("Listening to port %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Router))
}

func (server *Server) initializeDB(dbConfig DBConfig) {
	var err error
	if dbConfig.DBDriver == "mysql" {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", dbConfig.DBUser, dbConfig.DBPassword, dbConfig.DBHost, dbConfig.DBPort, dbConfig.DBName)
		server.DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	} else {
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Jakarta", dbConfig.DBHost, dbConfig.DBUser, dbConfig.DBPassword, dbConfig.DBName, dbConfig.DBPort)
		server.DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	}

	if err != nil {
		panic("Failed on connecting to the database server")
	}
}

func (server *Server) initializeAppConfig(appConfig AppConfig) {
	server.AppConfig = &appConfig
}

func (server *Server) dbMigrate() {
	for _, model := range models.RegisterModels() {
		err := server.DB.Debug().AutoMigrate(model.Model)

		if err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("Database migrated successfully.")
}

func (server *Server) InitCommands(config AppConfig, dbConfig DBConfig) {
	server.initializeDB(dbConfig)
	initSessionStore()

	cmdApp := cli.NewApp()
	cmdApp.Commands = []cli.Command{
		{
			Name: "db:migrate",
			Action: func(c *cli.Context) error {
				server.dbMigrate()
				return nil
			},
		},
		{
			Name: "db:seed",
			Action: func(c *cli.Context) error {
				err := seeders.DBSeed(server.DB)
				if err != nil {
					log.Fatal(err)
				}

				return nil
			},
		},
	}

	err := cmdApp.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func GetPaginationLinks(config *AppConfig, params PaginationParams) (PaginationLinks, error) {
	var links []PageLink

	totalPages := int32(math.Ceil(float64(params.TotalRows) / float64(params.PerPage)))

	for i := 1; int32(i) <= totalPages; i++ {
		links = append(links, PageLink{
			Page:          int32(i),
			Url:           fmt.Sprintf("%s/%s?page=%s", config.AppURL, params.Path, fmt.Sprint(i)),
			IsCurrentPage: int32(i) == params.CurrentPage,
		})
	}

	var nextPage int32
	var prevPage int32

	prevPage = 1
	nextPage = totalPages

	if params.CurrentPage > 2 {
		prevPage = params.CurrentPage - 1
	}

	if params.CurrentPage < totalPages {
		nextPage = params.CurrentPage + 1
	}

	return PaginationLinks{
		CurrentPage: fmt.Sprintf("%s/%s?page=%s", config.AppURL, params.Path, fmt.Sprint(params.CurrentPage)),
		NextPage:    fmt.Sprintf("%s/%s?page=%s", config.AppURL, params.Path, fmt.Sprint(nextPage)),
		PrevPage:    fmt.Sprintf("%s/%s?page=%s", config.AppURL, params.Path, fmt.Sprint(prevPage)),
		TotalRows:   params.TotalRows,
		TotalPages:  totalPages,
		Links:       links,
	}, nil
}

func (server *Server) GetProvinces() ([]models.Province, error) {
	base := os.Getenv("API_ONGKIR_BASE_URL")
	key := os.Getenv("API_ONGKIR_KEY")

	// Tanpa API → jangan call http, kembalikan list kosong
	if base == "" || key == "" {
		return []models.Province{}, nil
	}

	url := fmt.Sprintf("%s/province?key=%s", strings.TrimRight(base, "/"), key)
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	provinceResponse := models.ProvinceResponse{}
	body, readErr := ioutil.ReadAll(response.Body)
	if readErr != nil {
		return nil, readErr
	}
	if err := json.Unmarshal(body, &provinceResponse); err != nil {
		return nil, err
	}
	return provinceResponse.ProvinceData.Results, nil
}

func (server *Server) GetCitiesByProvinceID(provinceID string) ([]models.City, error) {
	base := os.Getenv("API_ONGKIR_BASE_URL")
	key := os.Getenv("API_ONGKIR_KEY")

	// Tanpa API → jangan call http, kembalikan list kosong
	if base == "" || key == "" {
		return []models.City{}, nil
	}

	url := fmt.Sprintf("%s/city?key=%s&province=%s", strings.TrimRight(base, "/"), key, provinceID)
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	cityResponse := models.CityResponse{}
	body, readErr := ioutil.ReadAll(response.Body)
	if readErr != nil {
		return nil, readErr
	}
	if err := json.Unmarshal(body, &cityResponse); err != nil {
		return nil, err
	}
	return cityResponse.CityData.Results, nil
}

func (server *Server) CalculateShippingFee(shippingParams models.ShippingFeeParams) ([]models.ShippingFeeOption, error) {
	// Validasi minimal
	if shippingParams.Weight <= 0 {
		return nil, errors.New("invalid weight")
	}

	base := os.Getenv("API_ONGKIR_BASE_URL")
	key := os.Getenv("API_ONGKIR_KEY")

	// ===== Fallback lokal TANPA API =====
	// Jika ENV kosong, hitung ongkir manual
	if base == "" || key == "" {
		// helper: biaya dasar per bobot (dalam rupiah), return int64
		calcBase := func(weight int) int64 {
			switch {
			case weight <= 1000:
				return 14000
			case weight <= 3000:
				return 26000
			default:
				extra := (weight - 3000 + 999) / 1000 // pembulatan ke atas per kg
				return 26000 + int64(extra)*8000
			}
		}
		baseFee := calcBase(shippingParams.Weight)

		return []models.ShippingFeeOption{
			{Service: "REG", Fee: baseFee},
			{Service: "YES", Fee: baseFee + 12000},
			{Service: "FREE", Fee: 0},
		}, nil
	}
	// ===== END Fallback =====

	// … kalau ENV ada, jalankan flow API seperti semula …
	params := url.Values{}
	params.Add("key", key)
	params.Add("origin", shippingParams.Origin)
	params.Add("destination", shippingParams.Destination)
	params.Add("weight", strconv.Itoa(shippingParams.Weight))
	params.Add("courier", shippingParams.Courier)

	resp, err := http.PostForm(base+"cost", params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ongkirResp models.OngkirResponse
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &ongkirResp); err != nil {
		return nil, err
	}

	var options []models.ShippingFeeOption
	for _, result := range ongkirResp.OngkirData.Results {
		for _, cost := range result.Costs {
			options = append(options, models.ShippingFeeOption{
				Service: cost.Service,
				Fee:     cost.Cost[0].Value, // int64
			})
		}
	}

	return options, nil
}

func SetFlash(w http.ResponseWriter, r *http.Request, name string, value string) {
	session, err := store.Get(r, sessionFlash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session.AddFlash(value, name)
	session.Save(r, w)
}

func GetFlash(w http.ResponseWriter, r *http.Request, name string) []string {
	session, err := store.Get(r, sessionFlash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}

	fm := session.Flashes(name)
	if len(fm) == 0 {
		return nil
	}

	session.Save(r, w)
	var flashes []string
	for _, fl := range fm {
		flashes = append(flashes, fl.(string))
	}

	return flashes
}

func IsLoggedIn(r *http.Request) bool {
	if store == nil { // guard
		return false
	}
	session, _ := store.Get(r, sessionUser)
	return session.Values["id"] != nil
}

func ComparePassword(password string, hashedPassword string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)) == nil
}

func MakePassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	return string(hashedPassword), err
}

func (server *Server) CurrentUser(w http.ResponseWriter, r *http.Request) *models.User {
	if !IsLoggedIn(r) {
		return nil
	}

	session, _ := store.Get(r, sessionUser)

	userModel := models.User{}
	user, err := userModel.FindByID(server.DB, session.Values["id"].(string))
	if err != nil {
		session.Values["id"] = nil
		session.Save(r, w)
		return nil
	}

	return user
}

func (server *Server) GetCartCount(w http.ResponseWriter, r *http.Request) int {
	cartID := GetShoppingCartID(w, r)
	cart, _ := GetShoppingCart(server.DB, cartID)

	if cart == nil {
		return 0
	}

	cart, err := GetShoppingCart(server.DB, cartID)
	if err != nil || cart == nil {
		return 0
	}

	// GetItems butuh db dan cartID
	items, _ := cart.GetItems(server.DB, cartID)
	return len(items)
}

// ===== Admin helper =====
func IsAdminUser(u *models.User) bool {
	if u == nil {
		return false
	}
	adminEmail := os.Getenv("ADMIN_EMAIL") // contoh: admin@example.com
	return strings.EqualFold(strings.TrimSpace(u.Email), strings.TrimSpace(adminEmail))
}

var templateFuncs = []template.FuncMap{
	{
		"add": func(a, b int) int {
			return a + b
		},
	},
}

// InjectNavbarBadges: supaya badge unread selalu tersedia di layout.html
func (s *Server) InjectNavbarBadges(data map[string]interface{}, user *models.User) {
	if data == nil {
		return
	}

	// default supaya template aman
	data["userUnread"] = int64(0)
	data["totalUnread"] = int64(0)

	if user == nil {
		return
	}

	// ===== user unread (admin -> user) =====
	// hitung semua pesan dari ADMIN yang belum dibaca user pada chat milik user tsb
	var userUnread int64
	_ = s.DB.Raw(`
		SELECT COUNT(*)
		FROM chat_messages m
		JOIN chats c ON c.id = m.chat_id
		WHERE c.user_id = ?
		  AND m.sender_role = 'admin'
		  AND (c.user_last_read_at IS NULL OR m.created_at > c.user_last_read_at)
	`, user.ID).Scan(&userUnread).Error
	data["userUnread"] = userUnread

	// ===== admin unread (user -> admin) =====
	if IsAdminUser(user) {
		var totalUnread int64
		_ = s.DB.Raw(`
			SELECT COUNT(*)
			FROM chat_messages m
			JOIN chats c ON c.id = m.chat_id
			WHERE m.sender_role = 'user'
			  AND (c.admin_last_read_at IS NULL OR m.created_at > c.admin_last_read_at)
		`).Scan(&totalUnread).Error
		data["totalUnread"] = totalUnread
	}
}

// formatRupiah: helper untuk format harga jadi Rp xxx
func formatRupiah(price interface{}) string {
	switch v := price.(type) {
	case int:
		return fmt.Sprintf("Rp %d", v)
	case int64:
		return fmt.Sprintf("Rp %d", v)
	case float64:
		return fmt.Sprintf("Rp %d", int64(v))
	default:
		// fallback kalau tipenya bukan angka (misal decimal.Decimal, dll)
		return fmt.Sprintf("Rp %v", v)
	}
}

func userRender() *render.Render {
	funcMap := template.FuncMap{
		"formatRupiah": formatRupiah,

		// buat semua helper yang dipakai di template orders.html
		"lower": strings.ToLower,
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"seq": func(from, to int) []int {
			if to < from {
				return []int{}
			}
			s := make([]int, 0, to-from+1)
			for i := from; i <= to; i++ {
				s = append(s, i)
			}
			return s
		},
		"urlValuesWithPage": func(v url.Values, page int) string {
			// salin manual semua query param
			q := url.Values{}
			for key, vals := range v {
				for _, val := range vals {
					q.Add(key, val)
				}
			}

			// set / override param page
			q.Set("page", strconv.Itoa(page))
			return q.Encode()
		},
	}

	return render.New(render.Options{
		Directory:  "templates", // <- penting: pakai folder templates
		Layout:     "layout",
		Extensions: []string{".html", ".tmpl"},
		Funcs:      []template.FuncMap{funcMap},
	})
}

func (s *Server) RenderTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	t, err := template.ParseFiles("templates/" + tmpl)
	if err != nil {
		http.Error(w, "error load template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := t.Execute(w, data); err != nil {
		http.Error(w, "error execute template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (server *Server) RequireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsLoggedIn(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
