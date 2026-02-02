#!/bin/bash

# Generate 50 dummy records
for i in {1..50}; do
    curl -X POST http://localhost:5555/command \
        -H "Content-Type: application/json" \
        -d '{
            "host_ip": "192.168.1.'$((50 + i))'",
            "timestamp": "'$(date -u +'%Y-%m-%dT%H:%M:%SZ')'",
            "base_path": "C:\\screen-capturer",
            "total_directories": 3,
            "directories": [
              {"path": "C:\\screen-capturer\\Folder1", "file_count": '$((RANDOM % 200 + 100))', "size_bytes": 524288000, "size_mb": 500},
              {"path": "C:\\screen-capturer\\Folder2", "file_count": '$((RANDOM % 300 + 200))', "size_bytes": 1073741824, "size_mb": 1024},
              {"path": "C:\\screen-capturer\\Folder3", "file_count": '$((RANDOM % 150 + 50))', "size_bytes": 262144000, "size_mb": 250}
            ]
        }'
    
    echo "Record $i created"
    sleep 0.1
done

echo "All 50 records created successfully!"