package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
)

// Semua validasi dan penyimpanan ada di internal/admin; handler hanya membaca form, memanggil service, dan menampilkan hasilnya.

// pengguna

type usersPageData struct {
	Users  []admin.User
	SelfID int64
}

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	users, err := s.Admin.ListUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "admin_users", "Pengguna", "users", usersPageData{Users: users, SelfID: currentUser(r).ID})
}

func (s *Server) usersCreate(w http.ResponseWriter, r *http.Request) {
	_, err := s.Admin.CreateUser(r.Context(), strings.TrimSpace(r.PostFormValue("username")), r.PostFormValue("password"), r.PostFormValue("is_admin") == "on")
	s.done(w, r, "/admin/users", "Pengguna berhasil dibuat.", err)
}

func (s *Server) usersDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if id == currentUser(r).ID {
		s.done(w, r, "/admin/users", "", badInput("Anda tidak dapat menghapus akun yang sedang dipakai."))
		return
	}
	s.done(w, r, "/admin/users", "Pengguna dihapus.", s.Admin.DeleteUser(r.Context(), id))
}

// group

func (s *Server) groupsPage(w http.ResponseWriter, r *http.Request) {
	groups, err := s.Admin.ListGroups(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "admin_groups", "Group", "groups", groups)
}

func (s *Server) groupsCreate(w http.ResponseWriter, r *http.Request) {
	_, err := s.Admin.CreateGroup(r.Context(), r.PostFormValue("name"))
	s.done(w, r, "/admin/groups", "Group berhasil dibuat.", err)
}

func (s *Server) groupsDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	s.done(w, r, "/admin/groups", "Group dihapus.", s.Admin.DeleteGroup(r.Context(), id))
}

type groupPageData struct {
	Group       *admin.Group
	Members     []admin.User
	Candidates  []admin.User // pengguna yang belum menjadi anggota
	Access      []admin.GroupAccess
	Connections []admin.Connection
}

func (s *Server) groupPage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	group, err := s.Admin.GetGroup(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	members, err := s.Admin.ListMembers(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	acc, err := s.Admin.ListGroupAccess(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	users, err := s.Admin.ListUsers(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	conns, err := s.Admin.ListConnections(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	isMember := make(map[int64]bool, len(members))
	for _, m := range members {
		isMember[m.ID] = true
	}
	var candidates []admin.User
	for _, u := range users {
		if !isMember[u.ID] {
			candidates = append(candidates, u)
		}
	}
	s.render(w, r, http.StatusOK, "admin_group", group.Name, "groups",
		groupPageData{Group: group, Members: members, Candidates: candidates, Access: acc, Connections: conns})
}

func groupURL(r *http.Request) string { return "/admin/groups/" + r.PathValue("id") }

func (s *Server) membersAdd(w http.ResponseWriter, r *http.Request) {
	gid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	uid, ok := formInt64(r, "user_id")
	if !ok {
		s.done(w, r, groupURL(r), "", badInput("Pilih pengguna yang akan ditambahkan."))
		return
	}
	s.done(w, r, groupURL(r), "Anggota ditambahkan.", s.Admin.AddMember(r.Context(), gid, uid))
}

func (s *Server) membersRemove(w http.ResponseWriter, r *http.Request) {
	gid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	uid, ok := s.pathID(w, r, "userID")
	if !ok {
		return
	}
	s.done(w, r, groupURL(r), "Anggota dikeluarkan dari group.", s.Admin.RemoveMember(r.Context(), gid, uid))
}

func (s *Server) accessGrant(w http.ResponseWriter, r *http.Request) {
	gid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	dbID, ok := formInt64(r, "database_id")
	if !ok {
		s.done(w, r, groupURL(r), "", badInput("Pilih database."))
		return
	}
	level, err := access.ParseLevel(r.PostFormValue("level"))
	if err != nil {
		s.done(w, r, groupURL(r), "", badInput("Pilih level akses."))
		return
	}
	s.done(w, r, groupURL(r), "Akses disimpan.", s.Admin.GrantAccess(r.Context(), gid, dbID, level))
}

func (s *Server) accessRevoke(w http.ResponseWriter, r *http.Request) {
	gid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	dbID, ok := s.pathID(w, r, "dbID")
	if !ok {
		return
	}
	s.done(w, r, groupURL(r), "Akses dicabut.", s.Admin.RevokeAccess(r.Context(), gid, dbID))
}

// koneksi target

func (s *Server) connectionsPage(w http.ResponseWriter, r *http.Request) {
	conns, err := s.Admin.ListConnections(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "admin_connections", "Koneksi database", "connections", conns)
}

func (s *Server) connectionsCreate(w http.ResponseWriter, r *http.Request) {
	in, err := connectionInput(r)
	if err != nil {
		s.done(w, r, "/admin/connections", "", err)
		return
	}
	_, err = s.Admin.CreateConnection(r.Context(), in)
	s.done(w, r, "/admin/connections", "Koneksi berhasil ditambahkan dan diaktifkan.", err)
}

func (s *Server) connectionsUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	in, err := connectionInput(r)
	if err != nil {
		s.done(w, r, "/admin/connections", "", err)
		return
	}
	_, err = s.Admin.UpdateConnection(r.Context(), id, in)
	s.done(w, r, "/admin/connections", "Koneksi berhasil diperbarui dan diuji.", err)
}

func connectionInput(r *http.Request) (admin.ConnectionInput, error) {
	port, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("port")))
	if err != nil {
		return admin.ConnectionInput{}, badInput("Port harus berupa angka.")
	}
	return admin.ConnectionInput{
		Name: r.PostFormValue("name"), Host: r.PostFormValue("host"), Port: port,
		Database: r.PostFormValue("database"), Username: r.PostFormValue("username"),
		Password: r.PostFormValue("password"), SSLMode: r.PostFormValue("ssl_mode"),
	}, nil
}

func (s *Server) connectionsTest(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	err := s.Admin.TestConnection(r.Context(), id)
	if err != nil && isConnFailed(err) {
		// Pesan generik: detail koneksi tidak boleh bocor ke UI.
		s.setFlash(w, "error", "Koneksi gagal. Periksa host, port, nama database, dan kredensial.")
		http.Redirect(w, r, "/admin/connections", http.StatusSeeOther)
		return
	}
	s.done(w, r, "/admin/connections", "Koneksi berhasil dan aktif.", err)
}

func (s *Server) connectionsDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	s.done(w, r, "/admin/connections", "Koneksi dihapus.", s.Admin.DeleteConnection(r.Context(), id))
}
