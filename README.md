# Kubernetes Real-time Log Catcher

Kubernetes Real-time Log Catcher adalah sebuah tool yang ditulis dalam Go untuk mengumpulkan log dari pod Kubernetes secara real-time. Tool ini menyimpan log dalam file harian terpisah untuk setiap pod, memungkinkan manajemen log yang efisien dan mudah.

## Fitur

- Pemantauan real-time terhadap pod Kubernetes
- Pemisahan log harian per pod
- Buffering untuk meningkatkan kinerja I/O
- Rotasi file log otomatis
- Konfigurasi fleksibel melalui command-line flags
- Penanganan konkurensi yang aman
- Periodic flushing untuk memastikan log ditulis secara teratur

## Persyaratan

- Go 1.16 atau lebih baru
- Akses ke cluster Kubernetes (baik di dalam cluster atau melalui kubeconfig)

## Instalasi

1. Clone repository ini:
   ```
   git clone https://github.com/yourusername/k8s-log-catcher.git
   ```

2. Masuk ke direktori proyek:
   ```
   cd k8s-log-catcher
   ```

3. Build proyek:
   ```
   go build -o k8s-log-catcher
   ```

## Penggunaan

Jalankan tool dengan perintah berikut:

```
./k8s-log-catcher [flags]
```

### Flags yang tersedia:

- `-kubeconfig`: Path ke file kubeconfig (default: `$HOME/.kube/config`)
- `-namespace`: Namespace untuk dimonitor (kosong untuk semua namespace)
- `-in-cluster`: Gunakan konfigurasi in-cluster (set ke true jika berjalan di dalam cluster)
- `-retention`: Periode retensi log (default: 7 hari)
- `-buffer-size`: Ukuran buffer untuk penulisan log (default: 1MB)
- `-flush-interval`: Interval untuk flush log ke disk (default: 5 detik)

Contoh:

```
./k8s-log-catcher -namespace=default -retention=72h -buffer-size=2097152 -flush-interval=10s
```

## Struktur Log

Log disimpan dalam struktur direktori berikut:

```
logs/
├── namespace1/
│   ├── deployment1/
│   │   ├── pod1_2024-09-13.log
│   │   └── pod1_2024-09-14.log
│   └── deployment2/
│       ├── pod2_2024-09-13.log
│       └── pod2_2024-09-14.log
└── namespace2/
    └── deployment3/
        ├── pod3_2024-09-13.log
        └── pod3_2024-09-14.log
```

## Kontribusi

Kontribusi selalu diterima! Silakan buka issue atau submit pull request jika Anda memiliki saran perbaikan atau penambahan fitur.

## Lisensi

Proyek ini dilisensikan di bawah MIT License. Lihat file `LICENSE` untuk detail lebih lanjut.
