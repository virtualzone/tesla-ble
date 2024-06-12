# Tesla BLE

1. Generate private key:

   ```openssl ecparam -genkey -name prime256v1 -noout > private.pem```
1. Generate public key:

   ```openssl ec -in private.pem -pubout > public.pem```
1. ...