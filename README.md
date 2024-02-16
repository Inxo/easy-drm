# Video Encryption App in Golang

This is a simple application written in Golang that encrypts and serves video files over HTTP. It utilizes AES encryption to securely transmit the video data. The application also includes middleware for handling Cross-Origin Resource Sharing (CORS).

## Usage

1. Ensure you have Go installed on your system.
2. Clone or download the repository.
3. Navigate to the directory containing the `main.go` file.
4. Run the following command to start the server:

    ```bash
    go run main.go
    ```

5. The server will start on `localhost:8080`.

## Endpoints

### `/video`

- **Method:** GET
- **Description:** Retrieves and encrypts the video file located at `/data/input.mp4`.
- **Response:** The encrypted video data is served with an `application/octet-stream` content type.

## AES Encryption

- The AES encryption key used by default is `your_secret_aes_key_32_charslong`. Please replace it with your own securely generated key for production use.

## Middleware

The application includes middleware for handling CORS (Cross-Origin Resource Sharing) to allow requests from different origins.

## Note

- This is a basic implementation and may require additional security measures and optimizations for production use.
- Ensure proper access controls and authentication mechanisms are implemented before deploying this application in a production environment.
