package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"xkiro-backend/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Store interface {
	// Users
	CreateUser(ctx context.Context, u *models.User) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	UpdateUser(ctx context.Context, u *models.User) error
	UpdateUserBalance(ctx context.Context, id string, balance float64, tokens int64) error

	// ApiKeys
	CreateApiKey(ctx context.Context, k *models.ApiKey) error
	GetApiKeysByUser(ctx context.Context, userID string) ([]models.ApiKey, error)
	GetApiKeyByValue(ctx context.Context, key string) (*models.ApiKey, error)
	RevokeApiKey(ctx context.Context, id, userID string) error
	UpdateApiKeyUsage(ctx context.Context, id string, cost float64) error

	// Models
	GetAllModels(ctx context.Context) ([]models.ModelItem, error)
	GetModelByID(ctx context.Context, id string) (*models.ModelItem, error)
	InsertModels(ctx context.Context, items []models.ModelItem) error

	// Pricing Tiers
	GetPricingTiers(ctx context.Context) ([]models.PricingTier, error)
	InsertPricingTiers(ctx context.Context, tiers []models.PricingTier) error

	// Deals
	GetDeals(ctx context.Context) ([]models.Deal, error)
	InsertDeals(ctx context.Context, deals []models.Deal) error

	// Status
	GetStatusServices(ctx context.Context) ([]models.StatusService, error)
	InsertStatusServices(ctx context.Context, services []models.StatusService) error

	// Contact
	CreateContactMessage(ctx context.Context, msg *models.ContactMessage) error

	// Usage Logs
	CreateUsageLog(ctx context.Context, log *models.UsageLog) error
	GetUsageLogsByUser(ctx context.Context, userID string) ([]models.UsageLog, error)
	GetAllUsageLogs(ctx context.Context) ([]models.UsageLog, error)

	// Topup Transactions
	CreateTopupTransaction(ctx context.Context, tx *models.TopupTransaction) error
	GetTopupTransactionsByUser(ctx context.Context, userID string) ([]models.TopupTransaction, error)

	// Giftcodes
	CreateGiftcode(ctx context.Context, code *models.Giftcode) error
	GetAllGiftcodes(ctx context.Context) ([]models.Giftcode, error)
	GetGiftcodeByCode(ctx context.Context, code string) (*models.Giftcode, error)
	RedeemGiftcode(ctx context.Context, codeStr string, userID string) (*models.Giftcode, error)
	DeleteGiftcode(ctx context.Context, id string) error
	AddUserGiftTokens(ctx context.Context, userID string, tokens int64) error
	ConsumeUserGiftTokens(ctx context.Context, userID string, tokens int64) error

	// Admin
	GetAllUsers(ctx context.Context) ([]models.User, error)
	GetAllApiKeys(ctx context.Context) ([]models.ApiKey, error)
}

// Global store instance
var DB Store

// MongoStore implementation
type MongoStore struct {
	client   *mongo.Client
	database *mongo.Database
}

// MemoryStore implementation for instant local fallback
type MemoryStore struct {
	mu           sync.RWMutex
	users        map[string]*models.User
	apiKeys      map[string]*models.ApiKey
	modelsList   []models.ModelItem
	pricingTiers []models.PricingTier
	deals        []models.Deal
	status       []models.StatusService
	messages     []models.ContactMessage
	usageLogs    []models.UsageLog
	topupTxs     []models.TopupTransaction
	giftcodes    map[string]*models.Giftcode
}

func loadEnvFile() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
}

func InitDB() Store {
	loadEnvFile()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		log.Printf("[DB] Attempting MongoDB Atlas connection...")
		client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
		if err == nil {
			if pingErr := client.Ping(ctx, nil); pingErr == nil {
				log.Println("[DB] ✅ Successfully connected to MongoDB Atlas Cloud Cluster!")
				mStore := &MongoStore{
					client:   client,
					database: client.Database("nornai"),
				}
				DB = mStore
				SeedData(mStore)
				return mStore
			} else {
				log.Printf("[DB] MongoDB Ping Warning: %v", pingErr)
			}
		} else {
			log.Printf("[DB] MongoDB Connect Error: %v", err)
		}
	} else {
		log.Println("[DB] ℹ️ No MONGO_URI provided in environment.")
	}

	log.Println("[DB] MongoDB not reachable. Using built-in high-performance MemoryStore (zero setup required).")
	memStore := &MemoryStore{
		users:     make(map[string]*models.User),
		apiKeys:   make(map[string]*models.ApiKey),
		giftcodes: make(map[string]*models.Giftcode),
	}
	DB = memStore
	SeedData(memStore)
	return memStore
}

// ================= MemoryStore Methods =================

func (m *MemoryStore) CreateUser(ctx context.Context, u *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.users {
		if existing.Email == u.Email {
			return fmt.Errorf("user already exists with this email")
		}
	}
	m.users[u.ID] = u
	return nil
}

func (m *MemoryStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Email == email {
			copy := *u
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (m *MemoryStore) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if u, ok := m.users[id]; ok {
		copy := *u
		return &copy, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (m *MemoryStore) UpdateUser(ctx context.Context, u *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.ID]; ok {
		m.users[u.ID] = u
		return nil
	}
	return fmt.Errorf("user not found")
}

func (m *MemoryStore) UpdateUserBalance(ctx context.Context, id string, balance float64, tokens int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		u.Balance = balance
		u.Tokens = tokens
		u.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("user not found")
}

func (m *MemoryStore) CreateApiKey(ctx context.Context, k *models.ApiKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiKeys[k.ID] = k
	return nil
}

func (m *MemoryStore) GetApiKeysByUser(ctx context.Context, userID string) ([]models.ApiKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []models.ApiKey
	for _, k := range m.apiKeys {
		if k.UserID == userID {
			res = append(res, *k)
		}
	}
	return res, nil
}

func (m *MemoryStore) GetApiKeyByValue(ctx context.Context, key string) (*models.ApiKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range m.apiKeys {
		if k.Key == key && k.Status == "active" {
			copy := *k
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("invalid or revoked API key")
}

func (m *MemoryStore) RevokeApiKey(ctx context.Context, id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.apiKeys[id]; ok && k.UserID == userID {
		k.Status = "revoked"
		return nil
	}
	return fmt.Errorf("key not found")
}

func (m *MemoryStore) UpdateApiKeyUsage(ctx context.Context, id string, cost float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.apiKeys[id]; ok {
		k.SpendUsed += cost
		now := time.Now()
		k.LastUsedAt = &now
		return nil
	}
	return fmt.Errorf("key not found")
}

func (m *MemoryStore) GetAllModels(ctx context.Context) ([]models.ModelItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modelsList, nil
}

func (m *MemoryStore) GetModelByID(ctx context.Context, id string) (*models.ModelItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.modelsList {
		if item.ID == id {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("model not found")
}

func (m *MemoryStore) InsertModels(ctx context.Context, items []models.ModelItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelsList = items
	return nil
}

func (m *MemoryStore) GetPricingTiers(ctx context.Context) ([]models.PricingTier, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pricingTiers, nil
}

func (m *MemoryStore) InsertPricingTiers(ctx context.Context, tiers []models.PricingTier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pricingTiers = tiers
	return nil
}

func (m *MemoryStore) GetDeals(ctx context.Context) ([]models.Deal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deals, nil
}

func (m *MemoryStore) InsertDeals(ctx context.Context, deals []models.Deal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deals = deals
	return nil
}

func (m *MemoryStore) GetStatusServices(ctx context.Context) ([]models.StatusService, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status, nil
}

func (m *MemoryStore) InsertStatusServices(ctx context.Context, services []models.StatusService) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = services
	return nil
}

func (m *MemoryStore) CreateContactMessage(ctx context.Context, msg *models.ContactMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, *msg)
	return nil
}

func (m *MemoryStore) CreateUsageLog(ctx context.Context, l *models.UsageLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usageLogs = append(m.usageLogs, *l)
	return nil
}

func (m *MemoryStore) GetUsageLogsByUser(ctx context.Context, userID string) ([]models.UsageLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []models.UsageLog
	for _, l := range m.usageLogs {
		if l.UserID == userID {
			res = append(res, l)
		}
	}
	return res, nil
}

func (m *MemoryStore) GetAllUsageLogs(ctx context.Context) ([]models.UsageLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usageLogs, nil
}

func (m *MemoryStore) CreateTopupTransaction(ctx context.Context, tx *models.TopupTransaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topupTxs = append(m.topupTxs, *tx)
	return nil
}

func (m *MemoryStore) GetTopupTransactionsByUser(ctx context.Context, userID string) ([]models.TopupTransaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []models.TopupTransaction
	for _, tx := range m.topupTxs {
		if tx.UserID == userID {
			res = append(res, tx)
		}
	}
	return res, nil
}

func (m *MemoryStore) GetAllUsers(ctx context.Context) ([]models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []models.User
	for _, u := range m.users {
		res = append(res, *u)
	}
	return res, nil
}

func (m *MemoryStore) GetAllApiKeys(ctx context.Context) ([]models.ApiKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []models.ApiKey
	for _, k := range m.apiKeys {
		res = append(res, *k)
	}
	return res, nil
}

func (m *MemoryStore) CreateGiftcode(ctx context.Context, code *models.Giftcode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.giftcodes[code.ID] = code
	return nil
}

func (m *MemoryStore) GetAllGiftcodes(ctx context.Context) ([]models.Giftcode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []models.Giftcode
	for _, g := range m.giftcodes {
		res = append(res, *g)
	}
	return res, nil
}

func (m *MemoryStore) GetGiftcodeByCode(ctx context.Context, codeStr string) (*models.Giftcode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, g := range m.giftcodes {
		if strings.EqualFold(g.Code, codeStr) {
			copy := *g
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("giftcode not found")
}

func (m *MemoryStore) RedeemGiftcode(ctx context.Context, codeStr string, userID string) (*models.Giftcode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found *models.Giftcode
	for _, g := range m.giftcodes {
		if strings.EqualFold(g.Code, codeStr) {
			found = g
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("Mã Giftcode không tồn tại trên hệ thống")
	}
	if found.Status == "exhausted" || (found.MaxUses > 0 && found.UsedCount >= found.MaxUses) {
		return nil, fmt.Errorf("Mã Giftcode này đã hết số lượt sử dụng")
	}
	for _, u := range found.UsedBy {
		if u == userID {
			return nil, fmt.Errorf("Bạn đã nhập mã Giftcode này trước đó rồi")
		}
	}

	found.UsedBy = append(found.UsedBy, userID)
	found.UsedCount++
	if found.MaxUses > 0 && found.UsedCount >= found.MaxUses {
		found.Status = "exhausted"
	}

	if u, ok := m.users[userID]; ok {
		u.GiftTokens += found.Tokens
		u.UpdatedAt = time.Now()
	}

	copy := *found
	return &copy, nil
}

func (m *MemoryStore) DeleteGiftcode(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.giftcodes, id)
	return nil
}

func (m *MemoryStore) AddUserGiftTokens(ctx context.Context, userID string, tokens int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.GiftTokens += tokens
		u.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("user not found")
}

func (m *MemoryStore) ConsumeUserGiftTokens(ctx context.Context, userID string, tokens int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.GiftTokens -= tokens
		if u.GiftTokens < 0 {
			u.GiftTokens = 0
		}
		u.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("user not found")
}

// ================= MongoStore Methods =================

func (ms *MongoStore) CreateUser(ctx context.Context, u *models.User) error {
	coll := ms.database.Collection("users")
	_, err := coll.InsertOne(ctx, u)
	return err
}

func (ms *MongoStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	coll := ms.database.Collection("users")
	var u models.User
	err := coll.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (ms *MongoStore) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	coll := ms.database.Collection("users")
	var u models.User
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (ms *MongoStore) UpdateUser(ctx context.Context, u *models.User) error {
	coll := ms.database.Collection("users")
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": u.ID}, u, options.Replace().SetUpsert(true))
	return err
}

func (ms *MongoStore) UpdateUserBalance(ctx context.Context, id string, balance float64, tokens int64) error {
	coll := ms.database.Collection("users")
	_, err := coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"balance":    balance,
			"tokens":     tokens,
			"updated_at": time.Now(),
		},
	})
	return err
}

func (ms *MongoStore) CreateApiKey(ctx context.Context, k *models.ApiKey) error {
	coll := ms.database.Collection("api_keys")
	_, err := coll.InsertOne(ctx, k)
	return err
}

func (ms *MongoStore) GetApiKeysByUser(ctx context.Context, userID string) ([]models.ApiKey, error) {
	coll := ms.database.Collection("api_keys")
	cur, err := coll.Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.ApiKey
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) GetApiKeyByValue(ctx context.Context, key string) (*models.ApiKey, error) {
	coll := ms.database.Collection("api_keys")
	var k models.ApiKey
	err := coll.FindOne(ctx, bson.M{"key": key, "status": "active"}).Decode(&k)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (ms *MongoStore) RevokeApiKey(ctx context.Context, id, userID string) error {
	coll := ms.database.Collection("api_keys")
	_, err := coll.UpdateOne(ctx, bson.M{"_id": id, "user_id": userID}, bson.M{
		"$set": bson.M{"status": "revoked"},
	})
	return err
}

func (ms *MongoStore) UpdateApiKeyUsage(ctx context.Context, id string, cost float64) error {
	coll := ms.database.Collection("api_keys")
	_, err := coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$inc": bson.M{"spend_used": cost},
		"$set": bson.M{"last_used_at": time.Now()},
	})
	return err
}

func (ms *MongoStore) GetAllModels(ctx context.Context) ([]models.ModelItem, error) {
	coll := ms.database.Collection("models")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.ModelItem
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) GetModelByID(ctx context.Context, id string) (*models.ModelItem, error) {
	coll := ms.database.Collection("models")
	var item models.ModelItem
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&item)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (ms *MongoStore) InsertModels(ctx context.Context, items []models.ModelItem) error {
	coll := ms.database.Collection("models")
	for _, item := range items {
		_, _ = coll.ReplaceOne(ctx, bson.M{"_id": item.ID}, item, options.Replace().SetUpsert(true))
	}
	return nil
}

func (ms *MongoStore) GetPricingTiers(ctx context.Context) ([]models.PricingTier, error) {
	coll := ms.database.Collection("pricing_tiers")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.PricingTier
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) InsertPricingTiers(ctx context.Context, tiers []models.PricingTier) error {
	coll := ms.database.Collection("pricing_tiers")
	for _, t := range tiers {
		_, _ = coll.ReplaceOne(ctx, bson.M{"_id": t.ID}, t, options.Replace().SetUpsert(true))
	}
	return nil
}

func (ms *MongoStore) GetDeals(ctx context.Context) ([]models.Deal, error) {
	coll := ms.database.Collection("deals")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.Deal
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) InsertDeals(ctx context.Context, deals []models.Deal) error {
	coll := ms.database.Collection("deals")
	for _, d := range deals {
		_, _ = coll.ReplaceOne(ctx, bson.M{"_id": d.ID}, d, options.Replace().SetUpsert(true))
	}
	return nil
}

func (ms *MongoStore) GetStatusServices(ctx context.Context) ([]models.StatusService, error) {
	coll := ms.database.Collection("status_services")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.StatusService
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) InsertStatusServices(ctx context.Context, services []models.StatusService) error {
	coll := ms.database.Collection("status_services")
	for _, s := range services {
		_, _ = coll.ReplaceOne(ctx, bson.M{"name": s.Name}, s, options.Replace().SetUpsert(true))
	}
	return nil
}

func (ms *MongoStore) CreateContactMessage(ctx context.Context, msg *models.ContactMessage) error {
	coll := ms.database.Collection("contact_messages")
	_, err := coll.InsertOne(ctx, msg)
	return err
}

func (ms *MongoStore) CreateUsageLog(ctx context.Context, l *models.UsageLog) error {
	coll := ms.database.Collection("usage_logs")
	_, err := coll.InsertOne(ctx, l)
	return err
}

func (ms *MongoStore) GetUsageLogsByUser(ctx context.Context, userID string) ([]models.UsageLog, error) {
	coll := ms.database.Collection("usage_logs")
	cur, err := coll.Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.UsageLog
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) GetAllUsageLogs(ctx context.Context) ([]models.UsageLog, error) {
	coll := ms.database.Collection("usage_logs")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.UsageLog
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) GetAllUsers(ctx context.Context) ([]models.User, error) {
	coll := ms.database.Collection("users")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.User
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) GetAllApiKeys(ctx context.Context) ([]models.ApiKey, error) {
	coll := ms.database.Collection("api_keys")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.ApiKey
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) CreateTopupTransaction(ctx context.Context, tx *models.TopupTransaction) error {
	coll := ms.database.Collection("topup_transactions")
	_, err := coll.InsertOne(ctx, tx)
	return err
}

func (ms *MongoStore) GetTopupTransactionsByUser(ctx context.Context, userID string) ([]models.TopupTransaction, error) {
	coll := ms.database.Collection("topup_transactions")
	cur, err := coll.Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.TopupTransaction
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) CreateGiftcode(ctx context.Context, code *models.Giftcode) error {
	coll := ms.database.Collection("giftcodes")
	_, err := coll.InsertOne(ctx, code)
	return err
}

func (ms *MongoStore) GetAllGiftcodes(ctx context.Context) ([]models.Giftcode, error) {
	coll := ms.database.Collection("giftcodes")
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var res []models.Giftcode
	if err := cur.All(ctx, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (ms *MongoStore) GetGiftcodeByCode(ctx context.Context, codeStr string) (*models.Giftcode, error) {
	coll := ms.database.Collection("giftcodes")
	var code models.Giftcode
	err := coll.FindOne(ctx, bson.M{"code": bson.M{"$regex": "^" + codeStr + "$", "$options": "i"}}).Decode(&code)
	if err != nil {
		return nil, err
	}
	return &code, nil
}

func (ms *MongoStore) RedeemGiftcode(ctx context.Context, codeStr string, userID string) (*models.Giftcode, error) {
	code, err := ms.GetGiftcodeByCode(ctx, codeStr)
	if err != nil {
		return nil, fmt.Errorf("Mã Giftcode không tồn tại trên hệ thống")
	}

	if code.Status == "exhausted" || (code.MaxUses > 0 && code.UsedCount >= code.MaxUses) {
		return nil, fmt.Errorf("Mã Giftcode này đã hết số lượt sử dụng")
	}

	for _, u := range code.UsedBy {
		if u == userID {
			return nil, fmt.Errorf("Bạn đã sử dụng mã Giftcode này trước đó rồi")
		}
	}

	code.UsedBy = append(code.UsedBy, userID)
	code.UsedCount++
	if code.MaxUses > 0 && code.UsedCount >= code.MaxUses {
		code.Status = "exhausted"
	}

	coll := ms.database.Collection("giftcodes")
	_, err = coll.UpdateOne(ctx, bson.M{"_id": code.ID}, bson.M{
		"$set": bson.M{
			"used_count": code.UsedCount,
			"used_by":    code.UsedBy,
			"status":     code.Status,
		},
	})
	if err != nil {
		return nil, err
	}

	// Add GiftTokens to user in MongoDB
	_ = ms.AddUserGiftTokens(ctx, userID, code.Tokens)

	return code, nil
}

func (ms *MongoStore) DeleteGiftcode(ctx context.Context, id string) error {
	coll := ms.database.Collection("giftcodes")
	_, err := coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (ms *MongoStore) AddUserGiftTokens(ctx context.Context, userID string, tokens int64) error {
	coll := ms.database.Collection("users")
	_, err := coll.UpdateOne(ctx, bson.M{"_id": userID}, bson.M{
		"$inc": bson.M{"gift_tokens": tokens},
		"$set": bson.M{"updated_at": time.Now()},
	})
	return err
}

func (ms *MongoStore) ConsumeUserGiftTokens(ctx context.Context, userID string, tokens int64) error {
	coll := ms.database.Collection("users")
	_, err := coll.UpdateOne(ctx, bson.M{"_id": userID}, bson.M{
		"$inc": bson.M{"gift_tokens": -tokens},
		"$set": bson.M{"updated_at": time.Now()},
	})
	return err
}
