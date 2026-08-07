package database

import (
    "fmt"
    "os"
    "github.com/joho/godotenv"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
    "gorm.io/gorm/schema"
)

func InitDB() *gorm.DB {
    // 1. โหลดไฟล์ .env
    err := godotenv.Load()
    if err != nil {
        fmt.Println("⚠️ Warning: .env file not found, using system environment variables")
    }

    // 2. ดึงค่าจาก Environment Variables
    host := os.Getenv("DB_HOST")
    user := os.Getenv("DB_USER")
    password := os.Getenv("DB_PASSWORD")
    dbname := os.Getenv("DB_NAME")
    port := os.Getenv("DB_PORT")
    
    // ดึงค่า sslmode (แนะนำให้ใช้ require สำหรับ Neon)
    sslmode := os.Getenv("DB_SSLMODE")
    if sslmode == "" {
        sslmode = "require" // Default สำหรับ Neon.tech
    }

    // 3. ประกอบ DSN
    // เปลี่ยนจาก sslmode=disable เป็นค่าที่ดึงมาจาก env
    dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
        host, user, password, dbname, port, sslmode)

    // 4. ตั้งค่า GORM Config
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
        NamingStrategy: schema.NamingStrategy{
            // ไม่ใส่ TablePrefix เพื่อให้เราเรียกระบุ schema.table ได้ตรงๆ 
            // เช่น Table("auth.user_account")
            TablePrefix: "", 
            SingularTable: true, // ปิดการเติม 's' ท้ายชื่อตารางอัตโนมัติของ GORM
        },
        // ปิด FullSaveAssociations เพื่อความปลอดภัยในการ Update ข้อมูล
        FullSaveAssociations: false,
        // แปล error เฉพาะของ Postgres (เช่น unique violation) ให้เป็น error
        // กลางของ GORM (gorm.ErrDuplicatedKey ฯลฯ) จะได้เช็คด้วย errors.Is()
        // ในโค้ด handler ได้โดยไม่ต้อง import driver เฉพาะ (จำเป็นสำหรับ
        // LinkLineAccount ที่ต้องแยกเคส "LINE account ผูกซ้ำ" ออกจาก error อื่น)
        TranslateError: true,
    })

    if err != nil {
        // พยายาม print error ที่อ่านง่ายขึ้น
        fmt.Printf("❌ Database Connection Error: %v\n", err)
        panic("Failed to connect to database")
    }

    // 5. (Optional) ตั้งค่า Connection Pool เพื่อ Performance
    sqlDB, _ := db.DB()
    sqlDB.SetMaxIdleConns(10)
    sqlDB.SetMaxOpenConns(100)

    fmt.Println("✅ Database connected successfully to Neon Tech")
    return db
}