# YouTube Likes Transfer

Transfer YouTube (Music) likes from one Google account to another using Go and YouTube Data API v3.

## Prerequisites

- Go 1.19 or higher
- Google Cloud Console account
- Two YouTube/Google accounts (source and target)

## Setup

### 1. Enable YouTube Data API

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing
3. Navigate to **APIs & Services** → **Library**
4. Search for "YouTube Data API v3"
5. Click **Enable**

### 2. Create OAuth2 Credentials

1. Go to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **OAuth client ID**
3. If prompted, configure OAuth consent screen:
   - User Type: External
   - Fill required fields (app name, support email)
   - Add scopes: 
     - `https://www.googleapis.com/auth/youtube`
     - `https://www.googleapis.com/auth/youtube.readonly`
   - Add test users (both your accounts)
4. For OAuth client:
   - Application type: **Desktop app**
   - Name: "YouTube Transfer Client"
5. Download credentials as `credentials.json`

### 3. Install Dependencies

```bash
go get
```

### 4. Run the Program

```bash
go run ytmtransfer.go 
```

The program will:
1. Ask you to authenticate the **source account** (where likes are copied from)
2. Ask you to authenticate the **target account** (where likes are copied to)
3. Fetch all liked videos from source
4. Like them on target account

## Usage Notes

### First Run
- Browser windows will open for OAuth authentication
- Authorize both accounts when prompted
- Tokens are saved locally for future use

### Subsequent Runs
- Uses saved tokens (`source_token.json`, `target_token.json`)
- Delete token files to switch accounts

### API Quotas
- YouTube API daily quota: 10,000 units
- Each like operation: 50 units
- Maximum ~200 likes per day
- Run again next day to continue

## Troubleshooting

### "localhost refused to connect"
When browser shows this error, copy the authorization code from the URL:
```
http://localhost:PORT/?code=COPY_THIS_CODE&scope=...
```

### "quotaExceeded" Error
- Wait until next day (quota resets at midnight Pacific Time)
- Program automatically resumes

### "invalid_grant" Error
- Delete the relevant token file
- Re-authenticate the account

### Rate Limiting
Program includes delays to avoid rate limits. If you still get 429 errors, increase the sleep duration in the code.

## File Structure
```
youtube-transfer/
├── main.go                 # Main program
├── credentials.json        # OAuth2 credentials (don't commit!)
├── source_token.json       # Source account token (auto-generated)
├── target_token.json       # Target account token (auto-generated)
```

## Security Notes

- **Never commit** `credentials.json` or `*_token.json` files
- Add them to `.gitignore`:
  ```
  credentials.json
  *_token.json
  remaining_videos.json
  ```
- Tokens expire after some time - just delete and re-authenticate

## Limitations

- Only transfers "likes", not playlists or subscriptions
- Subject to YouTube API quotas
- Cannot transfer private or deleted videos
- May skip age-restricted content depending on account settings
