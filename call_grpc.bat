@echo off
grpcurl -plaintext -d @register.json 127.0.0.1:8081 storage.UsrService/Register