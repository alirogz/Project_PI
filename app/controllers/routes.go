package controllers

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (server *Server) initializeRoutes() {
	server.Router = mux.NewRouter()
	server.Router.HandleFunc("/", server.Home).Methods("GET")

	server.Router.HandleFunc("/login", server.Login).Methods("GET")
	server.Router.HandleFunc("/login", server.DoLogin).Methods("POST")
	server.Router.HandleFunc("/register", server.Register).Methods("GET")
	server.Router.HandleFunc("/register", server.DoRegister).Methods("POST")
	server.Router.HandleFunc("/logout", server.Logout).Methods("GET")

	server.Router.HandleFunc("/products", server.Products).Methods("GET")
	server.Router.HandleFunc("/products/{slug}", server.GetProductBySlug).Methods("GET")

	// CART
	// cart
	server.Router.HandleFunc("/carts", server.GetCart).Methods("GET")
	server.Router.HandleFunc("/carts", server.AddItemToCart).Methods("POST")
	server.Router.HandleFunc("/carts/update", server.UpdateCartItemQty).Methods("POST")
	server.Router.HandleFunc("/carts/remove", server.RemoveCartItem).Methods("POST")

	// ORDERS
	server.Router.HandleFunc("/orders", server.OrdersIndex).Methods("GET")
	server.Router.HandleFunc("/orders/checkout", server.Checkout).Methods("POST")
	server.Router.HandleFunc("/orders/{id}", server.ShowOrder).Methods("GET")

	server.Router.HandleFunc("/orders/{id}/pay-manual", server.PayManualForm).Methods("GET")
	server.Router.HandleFunc("/orders/{id}/pay-manual", server.PayManual).Methods("POST")
	server.Router.HandleFunc("/orders/{id}/payment-proof", server.UploadPaymentProof).Methods("POST")

	// SHIPPING (local, tanpa API)
	server.Router.HandleFunc("/shipping/options", server.ShippingOptions).Methods("GET")

	// MOCK PAYMENT
	server.Router.HandleFunc("/payments/mock", server.MockPay).Methods("POST")

	// STATIC FILES (CSS, JS, gambar di /public)
	staticFileDirectory := http.Dir("./public/")
	staticFileHandler := http.StripPrefix("/public/", http.FileServer(staticFileDirectory))
	server.Router.PathPrefix("/public/").Handler(staticFileHandler).Methods("GET")

	// UPLOADS (gambar produk yang di-upload dari admin)
	uploadDir := http.Dir("./public/uploads")
	uploadHandler := http.StripPrefix("/uploads/", http.FileServer(uploadDir))
	server.Router.PathPrefix("/uploads/").Handler(uploadHandler).Methods("GET")

	// =======================
	//      ADMIN ORDERS
	// =======================
	server.Router.HandleFunc("/admin/orders", server.AdminOrdersIndex).Methods("GET")
	server.Router.HandleFunc("/admin/orders/{id}", server.AdminOrdersShow).Methods("GET")
	server.Router.HandleFunc("/admin/orders/{id}/pay-manual", server.AdminPayManual).Methods("POST")
	server.Router.HandleFunc("/admin/orders/{id}/status", server.AdminUpdateStatus).Methods("POST")
	server.Router.HandleFunc("/admin/orders/{id}/payment/approve", server.AdminApprovePayment).Methods("POST")
	server.Router.HandleFunc("/admin/orders/{id}/payment/reject", server.AdminRejectPayment).Methods("POST")

	// =======================
	//      ADMIN PRODUCTS
	// =======================
	server.Router.HandleFunc("/admin/products", server.AdminProductsIndex).Methods("GET")
	server.Router.HandleFunc("/admin/products/new", server.AdminProductsNew).Methods("GET")
	server.Router.HandleFunc("/admin/products", server.AdminProductsCreate).Methods("POST")
	server.Router.HandleFunc("/admin/products/{id}/edit", server.AdminProductsEdit).Methods("GET")
	server.Router.HandleFunc("/admin/products/{id}", server.AdminProductsUpdate).Methods("POST")
	server.Router.HandleFunc("/admin/products/{id}/delete", server.AdminProductsDelete).Methods("POST")
	// Admin dashboard
	server.Router.HandleFunc("/admin/dashboard", server.AdminDashboard).Methods("GET")

	// =======================
	//      ADMIN PAYMENTS
	// =======================
	server.Router.HandleFunc("/admin/payments/auto-match", server.AutoMatchPayments).Methods("POST")
	server.Router.HandleFunc("/admin/payments/import", server.ShowImportBankPage).Methods("GET")
	server.Router.HandleFunc("/admin/payments/import", server.HandleImportBankCSV).Methods("POST")
	server.Router.HandleFunc("/admin/payments/bank-tx/debug", server.DebugListBankTx).Methods("GET")

	// PROFILE
	server.Router.HandleFunc("/profile", server.RequireLogin(server.ProfileIndex)).Methods("GET")
	server.Router.HandleFunc("/profile", server.RequireLogin(server.ProfileUpdate)).Methods("POST")

	// GANTI PASSWORD
	server.Router.HandleFunc("/profile/password", server.RequireLogin(server.ProfilePasswordForm)).Methods("GET")
	server.Router.HandleFunc("/profile/password", server.RequireLogin(server.ProfilePasswordUpdate)).Methods("POST")

	// Address routes
	server.Router.HandleFunc("/addresses", server.RequireLogin(server.AddressesIndex)).Methods("GET")
	server.Router.HandleFunc("/addresses/new", server.RequireLogin(server.AddressNew)).Methods("GET")
	server.Router.HandleFunc("/addresses", server.RequireLogin(server.AddressCreate)).Methods("POST")
	server.Router.HandleFunc("/addresses/{id}/edit", server.RequireLogin(server.AddressEdit)).Methods("GET")
	server.Router.HandleFunc("/addresses/{id}/update", server.RequireLogin(server.AddressUpdate)).Methods("POST")
	server.Router.HandleFunc("/addresses/{id}/delete", server.RequireLogin(server.AddressDelete)).Methods("POST")
	server.Router.HandleFunc("/addresses/{id}/default", server.RequireLogin(server.AddressMakeDefault)).Methods("POST")

	// =======================
	//         LIVE CHAT
	// =======================
	server.Router.HandleFunc("/chat", server.RequireLogin(server.ChatPage)).Methods("GET")
	server.Router.HandleFunc("/chat/messages", server.RequireLogin(server.ChatMessages)).Methods("GET")
	server.Router.HandleFunc("/chat/messages", server.RequireLogin(server.ChatSend)).Methods("POST")

	// =======================
	//      ADMIN LIVE CHAT
	// =======================
	server.Router.HandleFunc("/admin/chats", server.AdminChatsIndex).Methods("GET")
	server.Router.HandleFunc("/admin/chats/{id}", server.AdminChatsShow).Methods("GET")
	server.Router.HandleFunc("/admin/chats/{id}/messages", server.AdminChatMessages).Methods("GET")
	server.Router.HandleFunc("/admin/chats/{id}/messages", server.AdminChatSend).Methods("POST")

}
